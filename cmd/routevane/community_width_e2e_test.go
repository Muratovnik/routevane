package main

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/sources/httpfeed"
)

// A curated third-party feed of the kind the shipped lists read for a single
// service carries whole regional and cloud blocks next to narrow ones. The
// router file must carry only what the community bound admits (ADR 0056): a
// /8 in a static route table sends a region's traffic into the tunnel.
func TestCommunityFeedWideBlocksNeverReachTheRouterFile(t *testing.T) {
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("38.0.0.0/7\n60.0.0.0/8\n8.208.0.0/12\n60.1.0.0/16\n203.0.113.0/24\n"))
	}))
	defer feed.Close()
	roots := x509.NewCertPool()
	roots.AddCert(feed.Certificate())

	catalogRoot := t.TempDir()
	for _, directory := range []string{"builtin", "targets"} {
		if err := os.Mkdir(filepath.Join(catalogRoot, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	list := "id: curated\n" +
		"title: Curated\n" +
		"components:\n  web:\n    required: true\n" +
		"sources:\n  - id: curated-cidr\n    type: http\n    component: web\n    config:\n" +
		"      url: https://example.com/cidr\n      format: text\n      class: community\n"
	if err := os.WriteFile(filepath.Join(catalogRoot, "builtin", "curated.yaml"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogRoot, "targets", "keenetic.yaml"), []byte(keeneticTargetYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	dataRoot := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{
		FeedResolver: staticFeedResolver{hosts: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("203.0.113.10")}}},
		FeedDialer:   redirectDialer{target: feed.Listener.Addr().String()},
		FeedOptions:  httpfeed.Options{Validity: time.Hour, RootCAs: roots},
		Now:          func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) },
		Context:      context.Background(),
	}
	runCommand(t, deps, []string{"refresh", "--list", "curated", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	result := runCommand(t, deps, []string{"build", "--target", "keenetic", "--list", "curated", "--output", filepath.Join(dataRoot, "exports"), "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	payload, err := os.ReadFile(strings.TrimSpace(result.stdout))
	if err != nil {
		t.Fatal(err)
	}
	routes, err := keenetic.Parse(payload)
	if err != nil {
		t.Fatalf("independent Keenetic validation failed: %v", err)
	}
	got := make([]string, len(routes))
	for i, route := range routes {
		got[i] = route.Prefix.String()
	}
	slices.Sort(got)
	if want := []string{"203.0.113.0/24", "60.1.0.0/16"}; !slices.Equal(got, want) {
		t.Fatalf("router file routes = %v, want %v", got, want)
	}
}
