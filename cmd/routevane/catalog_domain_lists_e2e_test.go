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

// These minimal dialect samples exercise the shipped source configuration;
// they are not vendored upstream lists or a claim of exact ruleset translation.
func TestAdditionalShippedDomainListsPublishSupportedNamesEndToEnd(t *testing.T) {
	for _, candidate := range []struct {
		id, body string
		want     []string
		skipped  int
	}{
		{
			id:   "github-copilot",
			body: "githubcopilot.com\nfull:copilot-proxy.githubusercontent.com\nfull:copilot-telemetry-service.githubusercontent.com @ads\nfull:copilot-telemetry.githubusercontent.com @ads\n",
			want: []string{"copilot-proxy.githubusercontent.com", "copilot-telemetry-service.githubusercontent.com", "copilot-telemetry.githubusercontent.com", "githubcopilot.com"},
		},
		{
			id: "twitch", body: "twitch.tv\nfull:d1g1f25tn8m2e6.cloudfront.net\n",
			want: []string{"d1g1f25tn8m2e6.cloudfront.net", "twitch.tv"},
		},
		{
			id: "kinopub", body: "kino.pub\nkino.watch\nregexp:(\\w+)-static-[0-9]+\\.cdntogo\\.net$\n",
			want: []string{"kino.pub", "kino.watch"}, skipped: 1,
		},
	} {
		t.Run(candidate.id, func(t *testing.T) {
			catalog := filepath.Join(t.TempDir(), "catalog")
			if err := os.CopyFS(catalog, os.DirFS(filepath.Join("..", "..", "catalog"))); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(catalog, "builtin", candidate.id+".yaml")
			definition, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Keep the shipped path/format/class; only the TLS fixture host differs.
			if err := os.WriteFile(path, []byte(strings.Replace(string(definition), "https://raw.githubusercontent.com/", "https://example.com/", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			var failed atomic.Bool
			feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2fly/domain-list-community/master/data/"+candidate.id {
					t.Errorf("unselected source requested: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				if failed.Load() {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("unrelated.example\n"))
					return
				}
				_, _ = w.Write([]byte(candidate.body))
			}))
			defer feed.Close()
			roots := x509.NewCertPool()
			roots.AddCert(feed.Certificate())
			now := time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC)
			origin, cancel, done, stderr := startServeServerWithFeed(t, catalog, filepath.Join(t.TempDir(), "data"), &hostAddressResolver{}, func() time.Time { return now }, roots, feed.Listener.Addr().String())
			defer func() {
				cancel()
				if code := <-done; code != 0 {
					t.Errorf("serve code=%d stderr=%s", code, stderr.String())
				}
			}()
			refreshBody := postJSON(t, origin+"/v1/lists/"+candidate.id+"/refresh", `{}`)
			var refresh struct {
				Summary struct {
					Skipped int `json:"skipped_entries"`
				} `json:"refresh"`
			}
			if err := json.Unmarshal(refreshBody, &refresh); err != nil || refresh.Summary.Skipped != candidate.skipped {
				t.Fatalf("skipped diagnostic=%s err=%v", refreshBody, err)
			}
			profileID, outputID, subscription := createProfileOutput(t, origin, candidate.id, "keenetic-dns", candidate.id)
			built := guardedRefreshAndBuild(t, origin, profileID, outputID)
			artifact := downloadArtifact(t, origin, built.Artifact.ID)
			groups, err := keeneticdns.Parse(artifact.body)
			if err != nil || len(groups) != 1 || !strings.HasPrefix(groups[0].Name, "routevane-") || !slices.Equal(groups[0].Entries, candidate.want) {
				t.Fatalf("DNS artifact=%s err=%v", artifact.body, err)
			}
			clientOutput := addOutput(t, origin, profileID, "singbox")
			clientBuild := guardedRefreshAndBuild(t, origin, profileID, clientOutput)
			clientArtifact := downloadArtifact(t, origin, clientBuild.Artifact.ID)
			client, err := singbox.Parse(clientArtifact.body)
			if err != nil || !slices.Equal(client.Rule.DomainSuffix, candidate.want) || len(client.Rule.Domain) != 0 || len(client.Rule.IPCIDR) != 0 {
				t.Fatalf("suffix-only artifact=%s err=%v", clientArtifact.body, err)
			}
			before := httpGet(t, subscription, nil)
			failed.Store(true)
			if response := postGuarded(t, origin+"/v1/profiles/"+profileID+"/refresh"); response.status != http.StatusUnprocessableEntity {
				t.Fatalf("source failure=%d %s", response.status, response.body)
			}
			after := httpGet(t, subscription, nil)
			if before.status != http.StatusOK || after.status != http.StatusOK || string(after.body) != string(before.body) {
				t.Fatalf("source failure replaced published file: %d %s", after.status, after.body)
			}
			contents := httpGet(t, origin+"/v1/lists/"+candidate.id+"/contents", nil)
			if contents.status != http.StatusOK || strings.Contains(string(contents.body), "unrelated.example") {
				t.Fatalf("HTTP error body became a destination: %d %s", contents.status, contents.body)
			}
		})
	}
}
