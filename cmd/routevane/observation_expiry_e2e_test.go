package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// hostAddressResolver is a dns.Resolver whose host-to-address map can be
// replaced at any time, so a test can change what resolves mid-run.
type hostAddressResolver struct {
	mu    sync.RWMutex
	hosts map[string][]string
}

func (r *hostAddressResolver) LookupHost(_ context.Context, name string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addresses, ok := r.hosts[name]
	if !ok {
		return nil, context.DeadlineExceeded
	}
	return append([]string(nil), addresses...), nil
}

func (*hostAddressResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }

func (r *hostAddressResolver) set(hosts map[string][]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hosts = hosts
}

func TestServePublishesImmutableArtifactsAcrossExpiringYouTubeAndDiscordObservations(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{
		"youtube.expiry.test": {"192.0.2.10"},
		"discord.expiry.test": {"198.51.100.20"},
	})
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	profileID, outputID, subscriptionURL := createProfileOutput(t, origin, "Видео и общение", "keenetic", "youtube", "discord")
	if !strings.HasPrefix(subscriptionURL, origin+"/v1/subscriptions/rv1.") {
		t.Fatalf("subscription url=%q", subscriptionURL)
	}

	firstBuild := refreshAndBuild(t, origin, profileID, outputID)
	firstArtifact := httpGet(t, origin+"/v1/artifacts/"+firstBuild.Artifact.ID, nil)
	if firstArtifact.status != http.StatusOK {
		t.Fatalf("first artifact status=%d body=%s", firstArtifact.status, firstArtifact.body)
	}
	assertKeeneticRoutes(t, firstArtifact.body, "192.0.2.10/32", "198.51.100.20/32")

	now = now.Add(2*time.Hour + time.Second)
	resolver.set(map[string][]string{
		"youtube.expiry.test": {"203.0.113.30"},
		"discord.expiry.test": {"203.0.113.40"},
	})
	secondBuild := refreshAndBuild(t, origin, profileID, outputID)
	if firstBuild.Artifact.ID == secondBuild.Artifact.ID {
		t.Fatal("changed, expired observations reused the old artifact")
	}
	secondArtifact := httpGet(t, origin+"/v1/artifacts/"+secondBuild.Artifact.ID, nil)
	if secondArtifact.status != http.StatusOK {
		t.Fatalf("second artifact status=%d body=%s", secondArtifact.status, secondArtifact.body)
	}
	assertKeeneticRoutes(t, secondArtifact.body, "203.0.113.30/32", "203.0.113.40/32")
	for _, expired := range []string{"192.0.2.10", "198.51.100.20"} {
		if strings.Contains(string(secondArtifact.body), expired) {
			t.Fatalf("artifact B retained expired address %q: %s", expired, secondArtifact.body)
		}
	}

	immutableA := httpGet(t, origin+"/v1/artifacts/"+firstBuild.Artifact.ID, nil)
	if immutableA.status != http.StatusOK || string(immutableA.body) != string(firstArtifact.body) {
		t.Fatalf("artifact A changed: status=%d body=%s", immutableA.status, immutableA.body)
	}
	subscription := httpGet(t, subscriptionURL, nil)
	if subscription.status != http.StatusOK || string(subscription.body) != string(secondArtifact.body) {
		t.Fatalf("stable subscription did not resolve to B: status=%d body=%s", subscription.status, subscription.body)
	}
}

type buildResponse struct {
	Artifact struct {
		ID string `json:"id"`
	} `json:"artifact"`
	Summary struct {
		ContentCreatedAt time.Time `json:"content_created_at"`
		ValidationStatus string    `json:"validation_status"`
	} `json:"summary"`
}

func refreshAndBuild(t *testing.T, origin, profileID, outputID string) buildResponse {
	t.Helper()
	postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
	payload := postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
	if strings.Contains(string(payload), `"routing_plan"`) {
		t.Fatalf("build response exposes raw plan: %s", payload)
	}
	var result buildResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Artifact.ID == "" || result.Summary.ContentCreatedAt.IsZero() || result.Summary.ValidationStatus != "valid" {
		t.Fatalf("build response did not contain bounded publication result: %s", payload)
	}
	return result
}

func assertKeeneticRoutes(t *testing.T, payload []byte, want ...string) {
	t.Helper()
	rules, err := keenetic.Parse(payload)
	if err != nil {
		t.Fatalf("Keenetic artifact invalid: %v\n%s", err, payload)
	}
	if len(rules) != len(want) {
		t.Fatalf("rules=%#v want=%v", rules, want)
	}
	for index, prefix := range want {
		if rules[index].Prefix.String() != prefix {
			t.Fatalf("rule[%d]=%s want=%s", index, rules[index].Prefix, prefix)
		}
	}
}

func startServeServerWithHostResolver(t *testing.T, catalog, data string, resolver *hostAddressResolver, now func() time.Time) (string, context.CancelFunc, <-chan int, *syncBuffer) {
	t.Helper()
	deps := runtimeDeps{Resolver: resolver, Now: now}
	return startServeServer(t, catalog, data, deps)
}
