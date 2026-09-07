package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/sources/dns"
)

type syncBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *syncBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

func TestServeCreateRefreshBuildSubscriptionAndReopen(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
	var contenderOut, contenderErr bytes.Buffer
	contenderListened := false
	contenderCode := runWithDeps(&contenderOut, &contenderErr, []string{"serve", "--catalog-dir", catalog, "--data-dir", data}, runtimeDeps{Resolver: &alphaBetaResolver{}, Now: func() time.Time { return now }, Context: context.Background(), Listen: func(string, string) (net.Listener, error) {
		contenderListened = true
		return nil, errors.New("must not listen")
	}})
	if contenderCode != 1 || contenderListened || contenderOut.Len() != 0 || !strings.Contains(contenderErr.String(), `"error_code":"server_lock_unavailable"`) {
		t.Fatalf("lock contender code=%d stdout=%q stderr=%q", contenderCode, contenderOut.String(), contenderErr.String())
	}
	createdProfile := postJSON(t, origin+"/v1/profiles", `{"name":"Альфа","lists":["alpha"]}`)
	var profileResponse struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(createdProfile, &profileResponse); err != nil {
		t.Fatal(err)
	}
	created := postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/outputs", `{"target_id":"keenetic"}`)
	var response struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	if err := json.Unmarshal(created, &response); err != nil {
		t.Fatal(err)
	}
	if len(profileResponse.Profile.ID) != 32 || len(response.Output.ID) != 32 || bytes.Contains(created, []byte("subscription_url")) {
		t.Fatalf("create response=%s / %s", createdProfile, created)
	}
	profileRead := httpGet(t, origin+"/v1/profiles/"+profileResponse.Profile.ID, nil)
	if profileRead.status != 200 || bytes.Contains(profileRead.body, []byte("subscription_url")) {
		t.Fatalf("list repeats secret: %s", profileRead.body)
	}
	postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/refresh", `{}`)
	built := postJSON(t, origin+"/v1/outputs/"+response.Output.ID+"/build", `{}`)
	var buildResponse struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	if err := json.Unmarshal(built, &buildResponse); err != nil || !strings.HasPrefix(buildResponse.SubscriptionURL, origin+"/v1/subscriptions/rv1.") {
		t.Fatalf("build response=%s err=%v", built, err)
	}
	subscriptionURL := buildResponse.SubscriptionURL
	first := httpGet(t, subscriptionURL, nil)
	if first.status != 200 || !strings.Contains(string(first.body), "route ADD 192.0.2.10 MASK 255.255.255.255 0.0.0.0\r\n") {
		t.Fatalf("subscription=%d %q", first.status, first.body)
	}
	second := httpGet(t, subscriptionURL, map[string]string{"If-None-Match": first.etag})
	if second.status != 304 || len(second.body) != 0 {
		t.Fatalf("conditional=%d %q", second.status, second.body)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("first server code=%d stderr=%s", code, stderr.String())
	}
	databaseBytes, err := os.ReadFile(filepath.Join(data, "routevane.db"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(subscriptionURL)
	token := strings.TrimPrefix(parsed.Path, "/v1/subscriptions/")
	if bytes.Contains(databaseBytes, []byte(token)) {
		t.Fatal("plaintext subscription token stored in database")
	}
	if strings.Contains(stderr.String(), token) {
		t.Fatal("plaintext subscription token logged")
	}

	reopenedOrigin, reopenedCancel, reopenedDone, reopenedErr := startAlphaBetaServeServer(t, catalog, data, now.Add(time.Hour))
	reopenedURL := reopenedOrigin + parsed.Path
	reopened := httpGet(t, reopenedURL, nil)
	if reopened.status != 200 || !bytes.Equal(reopened.body, first.body) || reopened.etag != first.etag || reopened.lastModified != first.lastModified {
		t.Fatalf("reopened=%d etag=%q/%q last=%q/%q body=%q", reopened.status, reopened.etag, first.etag, reopened.lastModified, first.lastModified, reopened.body)
	}
	reopenedCancel()
	if code := <-reopenedDone; code != 0 {
		t.Fatalf("reopened code=%d stderr=%s", code, reopenedErr.String())
	}
}

func TestFailedInitialBuildPersistsReasonWithoutIssuingSubscription(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	listIDs := make([]string, 0, 9)
	for listIndex := 1; listIndex <= 9; listIndex++ {
		listID := fmt.Sprintf("bulk%d", listIndex)
		listIDs = append(listIDs, listID)
		var yaml strings.Builder
		fmt.Fprintf(&yaml, "id: %s\ntitle: Bulk %d\ncomponents:\n  web:\n    required: false\nseeds:\n", listID, listIndex)
		for addressIndex := 0; addressIndex < 128; addressIndex++ {
			fmt.Fprintf(&yaml, "  - kind: ipv4\n    value: 8.%d.%d.1\n    component: web\n    source: manual\n", listIndex, addressIndex)
		}
		if err := os.WriteFile(filepath.Join(catalog, "builtin", listID+".yaml"), []byte(yaml.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()
	encodedLists, err := json.Marshal(listIDs)
	if err != nil {
		t.Fatal(err)
	}
	createdProfile := postJSON(t, origin+"/v1/profiles", `{"name":"Too many","lists":`+string(encodedLists)+`}`)
	var profileResponse struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(createdProfile, &profileResponse); err != nil {
		t.Fatal(err)
	}
	createdOutput := postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/outputs", `{"target_id":"keenetic"}`)
	var outputResponse struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	if err := json.Unmarshal(createdOutput, &outputResponse); err != nil {
		t.Fatal(err)
	}
	postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/refresh", `{}`)
	failed := postGuardedBody(t, origin+"/v1/outputs/"+outputResponse.Output.ID+"/build", `{}`)
	if failed.status != http.StatusUnprocessableEntity {
		t.Fatalf("failed build status=%d body=%s", failed.status, failed.body)
	}
	var problem struct {
		Code           string `json:"code"`
		ProjectedRules int    `json:"projected_rules"`
		MaximumRules   int    `json:"maximum_rules"`
	}
	if err := json.Unmarshal(failed.body, &problem); err != nil || problem.Code != application.BuildFailureRuleLimit || problem.ProjectedRules != 9*128 || problem.MaximumRules != 1024 || bytes.Contains(failed.body, []byte("subscription_url")) {
		t.Fatalf("failed build problem=%s decoded=%#v err=%v logs=%s", failed.body, problem, err, stderr.String())
	}
	detail := httpGet(t, origin+"/v1/profiles/"+profileResponse.Profile.ID, nil)
	var profileDetail struct {
		Outputs []struct {
			Latest      any `json:"latest"`
			LastAttempt struct {
				Status         string `json:"status"`
				Code           string `json:"code"`
				ProjectedRules int    `json:"projected_rules"`
				MaximumRules   int    `json:"maximum_rules"`
			} `json:"last_attempt"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(detail.body, &profileDetail); err != nil || len(profileDetail.Outputs) != 1 || profileDetail.Outputs[0].Latest != nil || profileDetail.Outputs[0].LastAttempt.Status != "failed" || profileDetail.Outputs[0].LastAttempt.Code != application.BuildFailureRuleLimit || profileDetail.Outputs[0].LastAttempt.ProjectedRules != 9*128 || profileDetail.Outputs[0].LastAttempt.MaximumRules != 1024 {
		t.Fatalf("persisted failure=%s decoded=%#v err=%v", detail.body, profileDetail, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(data, sqlite.DatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var subscriptions int
	if err := db.QueryRow(`SELECT count(*) FROM subscriptions`).Scan(&subscriptions); err != nil || subscriptions != 0 {
		t.Fatalf("subscriptions=%d err=%v", subscriptions, err)
	}
}

// createProfileOutput does over HTTP what the surface does: make a list, bind a
// format, refresh and complete its first publication. The subscription is
// therefore returned by the successful build, never by the unproven binding.
func createProfileOutput(t *testing.T, origin, name, targetID string, lists ...string) (profileID, outputID, subscriptionURL string) {
	t.Helper()
	encoded, err := json.Marshal(lists)
	if err != nil {
		t.Fatal(err)
	}
	var profileResponse struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	body := postJSON(t, origin+"/v1/profiles", `{"name":`+strconv.Quote(name)+`,"lists":`+string(encoded)+`,"priority":`+string(encoded)+`}`)
	if err := json.Unmarshal(body, &profileResponse); err != nil {
		t.Fatalf("create list=%s: %v", body, err)
	}
	var outputResponse struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	body = postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/outputs", `{"target_id":`+strconv.Quote(targetID)+`}`)
	if err := json.Unmarshal(body, &outputResponse); err != nil {
		t.Fatalf("create output=%s: %v", body, err)
	}
	if len(profileResponse.Profile.ID) != 32 || len(outputResponse.Output.ID) != 32 {
		t.Fatalf("create list/output identities: %s", body)
	}
	postJSON(t, origin+"/v1/profiles/"+profileResponse.Profile.ID+"/refresh", `{}`)
	buildBody := postJSON(t, origin+"/v1/outputs/"+outputResponse.Output.ID+"/build", `{}`)
	var buildResponse struct {
		SubscriptionURL string `json:"subscription_url"`
	}
	if err := json.Unmarshal(buildBody, &buildResponse); err != nil || !strings.HasPrefix(buildResponse.SubscriptionURL, origin+"/v1/subscriptions/rv1.") {
		t.Fatalf("initial build=%s err=%v", buildBody, err)
	}
	return profileResponse.Profile.ID, outputResponse.Output.ID, buildResponse.SubscriptionURL
}

type changingResolver struct{ address string }

func (r *changingResolver) LookupHost(context.Context, string) ([]string, error) {
	return []string{r.address}, nil
}
func (*changingResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }

func TestCorruptLatestFallsBackToIndependentlyVerifiedPrevious(t *testing.T) {
	catalogRoot := writeAlphaBetaCatalog(t)
	catalog, err := catalogyaml.Load(context.Background(), catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := catalog.Target("keenetic")
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resolver := &changingResolver{address: "192.0.2.10"}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	publication, err := application.NewPublicationService(application.PublicationConfig{Definitions: catalog.Lists, Targets: map[string]domain.TargetDefinition{target.ID: target}, TargetRevision: catalog.TargetRevision, Store: store, Files: filesystem.PublishedStore{DataRoot: root}, Renderers: application.RendererRegistry{keenetic.ID: keenetic.Renderer{}}, Sources: application.SourceRegistry{domain.SourceDNS: dns.Source{Observer: dns.NewObserver(resolver)}}, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := publication.CreateProfile(context.Background(), "Альфа", application.ProfileComposition{Lists: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	created, err := publication.AddOutput(context.Background(), profile.ID, "keenetic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publication.Refresh(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	a, err := publication.Build(context.Background(), created.Output.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := publication.IssueSubscription(context.Background(), created.Output.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	resolver.address = "198.51.100.20"
	if _, err := publication.Refresh(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	b, err := publication.Build(context.Background(), created.Output.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Artifact.ArtifactHash == b.Artifact.ArtifactHash {
		t.Fatal("changed observations did not change artifact")
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(b.Artifact.ArtifactPath)), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback, err := publication.Subscription(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if !fallback.Fallback || fallback.Artifact.ID != a.Artifact.ID {
		t.Fatalf("fallback=%#v", fallback)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(a.Artifact.ArtifactPath)), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := publication.Subscription(context.Background(), token); !errors.Is(err, application.ErrArtifactUnavailable) {
		t.Fatalf("both corrupt err=%v", err)
	}
}

func startAlphaBetaServeServer(t *testing.T, catalog, data string, now time.Time) (string, context.CancelFunc, <-chan int, *syncBuffer) {
	t.Helper()
	deps := runtimeDeps{Resolver: &alphaBetaResolver{}, Now: func() time.Time { return now }}
	return startServeServer(t, catalog, data, deps)
}

func postJSON(t *testing.T, endpoint, body string) []byte {
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
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("POST %s status=%d body=%s", endpoint, response.StatusCode, payload)
	}
	return payload
}

type httpResponse struct {
	status             int
	body               []byte
	etag, lastModified string
}

func httpGet(t *testing.T, endpoint string, headers map[string]string) httpResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResponse{response.StatusCode, payload, response.Header.Get("ETag"), response.Header.Get("Last-Modified")}
}
