package main

import (
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

func TestShippedCursorPublishesOnlyItsDomainsAndSurvivesSourceFailure(t *testing.T) {
	catalog := cursorFixtureCatalog(t)
	var failed atomic.Bool
	var fetched atomic.Int32
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2fly/domain-list-community/master/data/cursor" {
			t.Errorf("unexpected source fetch: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fetched.Add(1)
		if failed.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("cursor-cdn.com\ncursor.com\ncursor.sh\ncursorapi.com\n"))
	}))
	defer feed.Close()
	roots := x509.NewCertPool()
	roots.AddCert(feed.Certificate())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	origin, cancel, done, stderr := startServeServerWithFeed(t, catalog, filepath.Join(t.TempDir(), "data"), &hostAddressResolver{}, func() time.Time { return now }, roots, feed.Listener.Addr().String())
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Errorf("serve code=%d stderr=%s", code, stderr.String())
		}
	}()
	assertCursorLibrary(t, origin, true)
	listID, outputID, subscription := createListOutput(t, origin, "Cursor", "keenetic-dns", "cursor")
	built := guardedRefreshAndBuild(t, origin, listID, outputID)
	if built.Summary.RuleCount != 4 || len(built.Summary.DegradedSources) != 0 || fetched.Load() == 0 {
		t.Fatalf("healthy Cursor build=%#v fetches=%d", built, fetched.Load())
	}
	want := []string{"cursor-cdn.com", "cursor.com", "cursor.sh", "cursorapi.com"}
	artifact := downloadArtifact(t, origin, built.Artifact.ID)
	groups, err := keeneticdns.Parse(artifact.body)
	if artifact.status != http.StatusOK || err != nil || len(groups) != 1 || groups[0].Name != "routevane-cursor" || !slices.Equal(groups[0].Entries, want) {
		t.Fatalf("Cursor DNS artifact=%s err=%v", artifact.body, err)
	}
	clientOutput := addOutput(t, origin, listID, "singbox")
	clientBuild := guardedRefreshAndBuild(t, origin, listID, clientOutput)
	clientArtifact := downloadArtifact(t, origin, clientBuild.Artifact.ID)
	client, err := singbox.Parse(clientArtifact.body)
	if clientArtifact.status != http.StatusOK || err != nil || !slices.Equal(client.Rule.DomainSuffix, want) || len(client.Rule.Domain) != 0 || len(client.Rule.IPCIDR) != 0 {
		t.Fatalf("Cursor client artifact=%s err=%v", clientArtifact.body, err)
	}

	// A failed source refresh cannot replace a previously published file.
	published := httpGet(t, subscription, nil)
	failed.Store(true)
	refresh := postGuarded(t, origin+"/v1/profiles/"+listID+"/refresh")
	if refresh.status != http.StatusUnprocessableEntity || !strings.Contains(string(refresh.body), `"code":"source_unavailable"`) {
		t.Fatalf("failed source refresh=%d %s", refresh.status, refresh.body)
	}
	after := httpGet(t, subscription, nil)
	if published.status != http.StatusOK || after.status != http.StatusOK || string(after.body) != string(published.body) {
		t.Fatalf("source outage changed published Cursor: %d %s", after.status, after.body)
	}
	if old := downloadArtifact(t, origin, built.Artifact.ID); old.status != http.StatusOK || string(old.body) != string(artifact.body) {
		t.Fatalf("original Cursor artifact changed: %d %s", old.status, old.body)
	}
	if removed := postGuardedBody(t, origin+"/v1/lists/cursor/remove", `{}`); removed.status != http.StatusConflict || !strings.Contains(string(removed.body), listID) {
		t.Fatalf("a directly used Cursor list must name its owner: %d %s", removed.status, removed.body)
	}
}

func TestShippedCursorRemovalSurvivesCatalogReload(t *testing.T) {
	catalog := filepath.Join("..", "..", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	start := func() (string, func()) {
		origin, cancel, done, stderr := startServeServer(t, catalog, data, runtimeDeps{})
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			cancel()
			if code := <-done; code != 0 {
				t.Errorf("serve code=%d stderr=%s", code, stderr.String())
			}
		}
		t.Cleanup(stop)
		return origin, stop
	}
	origin, stop := start()
	assertCursorLibrary(t, origin, true)
	if removed := postGuardedBody(t, origin+"/v1/lists/cursor/remove", `{}`); removed.status != http.StatusNoContent {
		t.Fatalf("Cursor removal=%d %s", removed.status, removed.body)
	}
	assertCursorLibrary(t, origin, false)
	stop()
	restarted, _ := start()
	assertCursorLibrary(t, restarted, false)
}

func cursorFixtureCatalog(t *testing.T) string {
	t.Helper()
	catalog := filepath.Join(t.TempDir(), "catalog")
	if err := os.CopyFS(catalog, os.DirFS(filepath.Join("..", "..", "catalog"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(catalog, "builtin", "cursor.yaml")
	definition, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Change only the feed host to match the local TLS fixture certificate.
	// The shipped entry, categories, targets, path, parser and policy stay real.
	if err := os.WriteFile(path, []byte(strings.Replace(string(definition), "https://raw.githubusercontent.com/", "https://example.com/", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func assertCursorLibrary(t *testing.T, origin string, present bool) {
	t.Helper()
	response := httpGet(t, origin+"/v1/lists", nil)
	var library struct {
		Services []string `json:"lists"`
		Details  []struct {
			ID string `json:"id"`
		} `json:"list_details"`
		Categories []struct {
			ID       string   `json:"id"`
			Services []string `json:"lists"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(response.body, &library); response.status != http.StatusOK || err != nil {
		t.Fatalf("library=%d %s err=%v", response.status, response.body, err)
	}
	details := 0
	for _, entry := range library.Details {
		if entry.ID == "cursor" {
			details++
		}
	}
	wantCount := 0
	if present {
		wantCount = 1
	}
	if slices.Contains(library.Services, "cursor") != present || details != wantCount {
		t.Fatalf("Cursor present=%t details=%d; want present=%t", slices.Contains(library.Services, "cursor"), details, present)
	}
	foundDevelopment := false
	for _, category := range library.Categories {
		if category.ID == "development" {
			foundDevelopment = true
			if slices.Contains(category.Services, "cursor") != present {
				t.Fatalf("Development membership=%#v; Cursor present=%t", category.Services, present)
			}
		}
		if !present && slices.Contains(category.Services, "cursor") {
			t.Fatalf("removed Cursor remains in category %s", category.ID)
		}
	}
	if !foundDevelopment {
		t.Fatal("Development category missing")
	}
}
