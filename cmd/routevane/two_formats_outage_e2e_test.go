package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
	"github.com/Muratovnik/routevane/internal/sources/httpfeed"
)

// feedValidity is deliberately shorter than the grace window so the test can
// reach the state where an observation has expired but its source is still
// inside grace.
const feedValidity = time.Hour

// fixedFeedResolver answers with a documentation-range address so the
// destination policy accepts it without the test reaching a real host.
type fixedFeedResolver struct{}

func (fixedFeedResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host != "example.com" {
		return nil, context.DeadlineExceeded
	}
	return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
}

func TestPublishesOneProfileInTwoPracticallyDifferentFormatsAndSurvivesAFeedOutage(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "two-formats", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	var feedHealthy atomic.Bool
	feedHealthy.Store(true)
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !feedHealthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# operator published ranges\n198.51.100.0/24\n"))
	}))
	defer feed.Close()
	pool := x509.NewCertPool()
	pool.AddCert(feed.Certificate())

	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.two-formats.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithFeed(t, catalog, data, resolver, func() time.Time { return now }, pool, feed.Listener.Addr().String())
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("server stderr: %s", stderr.String())
		}
	})
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	targets := httpGet(t, origin+"/v1/targets", nil)
	if targets.status != http.StatusOK {
		t.Fatalf("targets status=%d body=%s", targets.status, targets.body)
	}
	var targetList struct {
		Targets []struct {
			ID         string `json:"id"`
			RendererID string `json:"renderer_id"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(targets.body, &targetList); err != nil {
		t.Fatal(err)
	}
	if len(targetList.Targets) != 2 {
		t.Fatalf("both catalog targets must be selectable: %s", targets.body)
	}
	byTarget := map[string]string{}
	for _, entry := range targetList.Targets {
		byTarget[entry.ID] = entry.RendererID
	}
	if byTarget["keenetic"] != keenetic.ID || byTarget["singbox"] != singbox.ID {
		t.Fatalf("targets = %#v", byTarget)
	}

	profileID, routerOutput, _ := createProfileOutput(t, origin, "YouTube", "keenetic", "youtube")
	clientOutput := addOutput(t, origin, profileID, "singbox")

	routerBuild := guardedRefreshAndBuild(t, origin, profileID, routerOutput)
	clientBuild := guardedRefreshAndBuild(t, origin, profileID, clientOutput)
	if len(routerBuild.Summary.DegradedSources) != 0 || len(clientBuild.Summary.DegradedSources) != 0 {
		t.Fatalf("a healthy feed must not report a degraded source: %#v %#v", routerBuild, clientBuild)
	}

	router := downloadArtifact(t, origin, routerBuild.Artifact.ID)
	client := downloadArtifact(t, origin, clientBuild.Artifact.ID)
	if router.contentType != "application/x-bat" || client.contentType != "application/json" {
		t.Fatalf("content types = %q %q", router.contentType, client.contentType)
	}
	if !strings.HasSuffix(router.filename, ".bat") || !strings.HasSuffix(client.filename, ".json") {
		t.Fatalf("download filenames = %q %q", router.filename, client.filename)
	}

	// Each artifact must satisfy its own independent validator, and neither
	// validator may accept the other format.
	routerRules, err := keenetic.Parse(router.body)
	if err != nil {
		t.Fatalf("router artifact invalid: %v\n%s", err, router.body)
	}
	clientRuleSet, err := singbox.Parse(client.body)
	if err != nil {
		t.Fatalf("client artifact invalid: %v\n%s", err, client.body)
	}
	if keenetic.Validate(client.body) == nil || singbox.Validate(router.body) == nil {
		t.Fatal("a format validator accepted the other format")
	}

	// The router dialect can only carry IPv4 routes, so the observed address and
	// the operator-declared range are all it holds.
	routerPrefixes := make([]string, 0, len(routerRules))
	for _, rule := range routerRules {
		routerPrefixes = append(routerPrefixes, rule.Prefix.String())
	}
	if strings.Join(routerPrefixes, ",") != "192.0.2.10/32,198.51.100.0/24" {
		t.Fatalf("router prefixes = %v", routerPrefixes)
	}

	// The rule-set format carries the domain suffix the router format has to
	// drop, and the domain makes the observed address unnecessary. Same profile
	// inputs, practically different output semantics.
	if strings.Join(clientRuleSet.Rule.DomainSuffix, ",") != "youtube.com" {
		t.Fatalf("client domain suffixes = %v", clientRuleSet.Rule.DomainSuffix)
	}
	if strings.Join(clientRuleSet.Rule.IPCIDR, ",") != "198.51.100.0/24" {
		t.Fatalf("client prefixes = %v", clientRuleSet.Rule.IPCIDR)
	}
	if strings.Contains(string(client.body), "192.0.2.10") {
		t.Fatalf("a domain-capable target must not need the observed address: %s", client.body)
	}
	if strings.Contains(string(router.body), "youtube.com") {
		t.Fatalf("the router dialect cannot carry a domain: %s", router.body)
	}

	// The feed now fails while its previous success is still inside the grace
	// window, and the feed observation itself has expired.
	feedHealthy.Store(false)
	now = now.Add(feedValidity + time.Minute)
	degradedBuild := guardedRefreshAndBuild(t, origin, profileID, routerOutput)
	if strings.Join(degradedBuild.Summary.DegradedSources, ",") != "official-ranges" {
		t.Fatalf("degraded sources = %#v", degradedBuild.Summary.DegradedSources)
	}
	degraded := downloadArtifact(t, origin, degradedBuild.Artifact.ID)
	if _, err := keenetic.Parse(degraded.body); err != nil {
		t.Fatalf("degraded artifact invalid: %v\n%s", err, degraded.body)
	}
	if !strings.Contains(string(degraded.body), "198.51.100.0") {
		t.Fatalf("grace must keep the operator range routable: %s", degraded.body)
	}

	// A source failure never removes a previously published artifact.
	retained := downloadArtifact(t, origin, routerBuild.Artifact.ID)
	if string(retained.body) != string(router.body) || retained.status != http.StatusOK {
		t.Fatalf("a source failure changed a published artifact: status=%d body=%s", retained.status, retained.body)
	}

	// After the grace window closes the still-failing source is a hard failure:
	// refresh reports it, and an explicit build publishes what the healthy
	// sources support instead of the expired range.
	now = now.Add(24 * time.Hour)
	hardFailure := postGuarded(t, origin+"/v1/profiles/"+profileID+"/refresh")
	if hardFailure.status != http.StatusUnprocessableEntity {
		t.Fatalf("an expired grace window must report a failed refresh: status=%d body=%s", hardFailure.status, hardFailure.body)
	}
	var expiredBuild degradedBuildResponse
	if err := json.Unmarshal(postJSON(t, origin+"/v1/outputs/"+routerOutput+"/build", `{}`), &expiredBuild); err != nil {
		t.Fatal(err)
	}
	if len(expiredBuild.Summary.DegradedSources) != 0 {
		t.Fatalf("grace must expire: %#v", expiredBuild.Summary.DegradedSources)
	}
	expired := downloadArtifact(t, origin, expiredBuild.Artifact.ID)
	if strings.Contains(string(expired.body), "198.51.100.0") {
		t.Fatalf("an expired grace window must drop the range: %s", expired.body)
	}
	if !strings.Contains(string(expired.body), "192.0.2.10") {
		t.Fatalf("the healthy DNS source must still contribute: %s", expired.body)
	}
	if third := downloadArtifact(t, origin, routerBuild.Artifact.ID); string(third.body) != string(router.body) {
		t.Fatalf("published artifacts must stay immutable: %s", third.body)
	}
	// With both sources unavailable and their grace exhausted, an address-only
	// candidate is empty. Refuse it without moving the last-good publication.
	resolver.set(nil)
	now = now.Add(48 * time.Hour)
	if failed := postGuarded(t, origin+"/v1/profiles/"+profileID+"/refresh"); failed.status != http.StatusUnprocessableEntity {
		t.Fatalf("all-source outage refresh=%d %s", failed.status, failed.body)
	}
	if failed := postGuarded(t, origin+"/v1/outputs/"+routerOutput+"/build"); failed.status != http.StatusUnprocessableEntity {
		t.Fatalf("empty candidate was accepted: %d %s", failed.status, failed.body)
	}
	var detail struct {
		Outputs []struct {
			ID     string `json:"id"`
			Latest struct {
				ID string `json:"id"`
			} `json:"latest"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(httpGet(t, origin+"/v1/profiles/"+profileID, nil).body, &detail); err != nil {
		t.Fatal(err)
	}
	latest := ""
	for _, output := range detail.Outputs {
		if output.ID == routerOutput {
			latest = output.Latest.ID
		}
	}
	if latest != expiredBuild.Artifact.ID {
		t.Fatalf("failed candidate moved publication: latest=%s", latest)
	}
	if retained := downloadArtifact(t, origin, expiredBuild.Artifact.ID); retained.status != http.StatusOK || string(retained.body) != string(expired.body) {
		t.Fatal("all-source outage changed the last-good artifact")
	}
}

// addOutput binds an existing profile to one more format. Two devices sharing
// one set of lists is the case the profile model exists for, so this test uses
// one profile with two outputs rather than two copies of the same composition.
func addOutput(t *testing.T, origin, profileID, targetID string) string {
	t.Helper()
	created := postJSON(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"`+targetID+`"}`)
	var response struct {
		Output struct {
			ID       string `json:"id"`
			TargetID string `json:"target_id"`
		} `json:"output"`
	}
	if err := json.Unmarshal(created, &response); err != nil {
		t.Fatal(err)
	}
	if response.Output.ID == "" || response.Output.TargetID != targetID || strings.Contains(string(created), "subscription_url") {
		t.Fatalf("output creation for %q = %s", targetID, created)
	}
	return response.Output.ID
}

type degradedBuildResponse struct {
	Artifact struct {
		ID          string `json:"id"`
		ContentType string `json:"content_type"`
		RendererID  string `json:"renderer_id"`
	} `json:"artifact"`
	Summary struct {
		RuleCount        int      `json:"rule_count"`
		ValidationStatus string   `json:"validation_status"`
		DegradedSources  []string `json:"degraded_sources"`
	} `json:"summary"`
}

func guardedRefreshAndBuild(t *testing.T, origin, profileID, outputID string) degradedBuildResponse {
	t.Helper()
	refreshed := postGuarded(t, origin+"/v1/profiles/"+profileID+"/refresh")
	if refreshed.status != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.status, refreshed.body)
	}
	payload := postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	var result degradedBuildResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Artifact.ID == "" || result.Summary.ValidationStatus != "valid" {
		t.Fatalf("build response = %s", payload)
	}
	return result
}

type artifactDownload struct {
	status                int
	body                  []byte
	contentType, filename string
}

func downloadArtifact(t *testing.T, origin, artifactID string) artifactDownload {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, origin+"/v1/artifacts/"+artifactID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := make([]byte, 0)
	buffer := make([]byte, 4096)
	for {
		read, readErr := response.Body.Read(buffer)
		body = append(body, buffer[:read]...)
		if readErr != nil {
			break
		}
	}
	disposition := response.Header.Get("Content-Disposition")
	filename := disposition
	if index := strings.Index(disposition, `filename="`); index >= 0 {
		filename = strings.TrimSuffix(disposition[index+len(`filename="`):], `"`)
	}
	return artifactDownload{status: response.StatusCode, body: body, contentType: response.Header.Get("Content-Type"), filename: filename}
}

// postGuarded issues a guarded POST and returns the response instead of failing
// on a non-2xx status, so a degraded or failed refresh can be asserted.
func postGuarded(t *testing.T, endpoint string) httpResponse {
	t.Helper()
	return postGuardedBody(t, endpoint, `{}`)
}

func postGuardedBody(t *testing.T, endpoint, body string) httpResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	received := make([]byte, 0)
	buffer := make([]byte, 4096)
	for {
		read, readErr := response.Body.Read(buffer)
		received = append(received, buffer[:read]...)
		if readErr != nil {
			break
		}
	}
	return httpResponse{status: response.StatusCode, body: received}
}

func startServeServerWithFeed(t *testing.T, catalog, data string, resolver *hostAddressResolver, now func() time.Time, roots *x509.CertPool, feedAddr string) (string, context.CancelFunc, <-chan int, *syncBuffer) {
	t.Helper()
	deps := runtimeDeps{
		Resolver:     resolver,
		FeedResolver: fixedFeedResolver{},
		FeedDialer:   redirectDialer{target: feedAddr},
		FeedOptions:  httpfeed.Options{Validity: feedValidity, RootCAs: roots},
		Now:          now,
	}
	return startServeServer(t, catalog, data, deps)
}
