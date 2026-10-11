package main

import (
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

func TestDisablingAFailedSourcePreservesTheActiveSourcesGraceWindow(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "two-formats", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	clock := &publicationTestClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	var feedHealthy atomic.Bool
	feedHealthy.Store(true)
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !feedHealthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("198.51.100.0/24\n"))
	}))
	defer feed.Close()
	pool := x509.NewCertPool()
	pool.AddCert(feed.Certificate())
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.two-formats.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithFeed(t, catalog, data, resolver, clock.read, pool, feed.Listener.Addr().String())
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()
	profileID, outputID, subscription := createProfileOutput(t, origin, "Grace", "keenetic", "youtube")
	initial := httpGet(t, subscription, nil)
	if initial.status != http.StatusOK || !strings.Contains(string(initial.body), "192.0.2.10") || !strings.Contains(string(initial.body), "198.51.100.0") {
		t.Fatalf("healthy sources were not both published: status=%d body=%s", initial.status, initial.body)
	}
	feedHealthy.Store(false)
	resolver.set(nil)
	clock.set(clock.read().Add(feedValidity + time.Minute))
	before := postGuarded(t, origin+"/v1/profiles/"+profileID+"/refresh")
	var refreshed struct {
		Refresh []application.RefreshSummary `json:"refresh"`
	}
	if err := json.Unmarshal(before.body, &refreshed); err != nil || before.status != http.StatusOK || len(refreshed.Refresh) != 1 || !reflect.DeepEqual(refreshed.Refresh[0].DegradedSources, []string{"dns-playback", "official-ranges"}) {
		t.Fatalf("two failing sources inside grace: status=%d body=%s err=%v", before.status, before.body, err)
	}
	postJSON(t, origin+"/v1/lists/youtube/sources/official-ranges/update", `{"enabled":false}`)
	clock.set(clock.read().Add(time.Minute))
	after := postGuarded(t, origin+"/v1/profiles/"+profileID+"/refresh")
	refreshed.Refresh = nil
	if err := json.Unmarshal(after.body, &refreshed); err != nil || after.status != http.StatusOK || len(refreshed.Refresh) != 1 {
		t.Fatalf("disabled source turned active grace into a hard failure: status=%d body=%s err=%v", after.status, after.body, err)
	}
	if refreshed.Refresh[0].FailedRuns != 1 || !reflect.DeepEqual(refreshed.Refresh[0].DegradedSources, []string{"dns-playback"}) {
		t.Fatalf("disabled source remained in active health: %#v", refreshed.Refresh[0])
	}
	var built degradedBuildResponse
	if err := json.Unmarshal(postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`), &built); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(built.Summary.DegradedSources, []string{"dns-playback"}) {
		t.Fatalf("published degraded sources=%v", built.Summary.DegradedSources)
	}
	payload := httpGet(t, subscription, nil)
	if payload.status != http.StatusOK || keenetic.Validate(payload.body) != nil || !strings.Contains(string(payload.body), "192.0.2.10") || strings.Contains(string(payload.body), "198.51.100.0") {
		t.Fatalf("active DNS grace and disabled feed were not separated: status=%d body=%s", payload.status, payload.body)
	}
}

// A feed listed two ranges, a later successful read listed only one, and then
// the feed failed inside its grace window. The degraded build keeps the range
// the last successful read reported and must not bring back the one it
// withdrew.
func TestSourceGraceDoesNotReviveAValueTheLastSuccessfulReadWithdrew(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "two-formats", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	clock := &publicationTestClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	const kept, withdrawn = "198.51.100.0", "203.0.113.0"
	var feedBody atomic.Value
	feedBody.Store(kept + "/24\n" + withdrawn + "/24\n")
	var feedHealthy atomic.Bool
	feedHealthy.Store(true)
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !feedHealthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(feedBody.Load().(string)))
	}))
	defer feed.Close()
	pool := x509.NewCertPool()
	pool.AddCert(feed.Certificate())
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.two-formats.test": {"192.0.2.10"}})
	origin, cancel, done, stderr := startServeServerWithFeed(t, catalog, data, resolver, clock.read, pool, feed.Listener.Addr().String())
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()
	profileID, outputID, _ := createProfileOutput(t, origin, "Withdrawn", "keenetic", "youtube")

	first := downloadArtifact(t, origin, guardedRefreshAndBuild(t, origin, profileID, outputID).Artifact.ID)
	if !strings.Contains(string(first.body), kept) || !strings.Contains(string(first.body), withdrawn) {
		t.Fatalf("the first read must publish both ranges: %s", first.body)
	}

	feedBody.Store(kept + "/24\n")
	clock.set(clock.read().Add(feedValidity + time.Minute))
	second := downloadArtifact(t, origin, guardedRefreshAndBuild(t, origin, profileID, outputID).Artifact.ID)
	if !strings.Contains(string(second.body), kept) || strings.Contains(string(second.body), withdrawn) {
		t.Fatalf("a successful read must drop the range it no longer lists: %s", second.body)
	}

	feedHealthy.Store(false)
	clock.set(clock.read().Add(feedValidity + time.Minute))
	degradedBuild := guardedRefreshAndBuild(t, origin, profileID, outputID)
	if !reflect.DeepEqual(degradedBuild.Summary.DegradedSources, []string{"official-ranges"}) {
		t.Fatalf("degraded sources = %#v", degradedBuild.Summary.DegradedSources)
	}
	degraded := downloadArtifact(t, origin, degradedBuild.Artifact.ID)
	if keenetic.Validate(degraded.body) != nil || !strings.Contains(string(degraded.body), kept) || !strings.Contains(string(degraded.body), "192.0.2.10") {
		t.Fatalf("grace must keep the last successful read and the healthy source: %s", degraded.body)
	}
	if strings.Contains(string(degraded.body), withdrawn) {
		t.Fatalf("grace revived a range the last successful read withdrew: %s", degraded.body)
	}
}
