package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/discovery"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/planner"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// staticHostResolver is a dns.Resolver fixed to the host-address map it was
// constructed with.
type staticHostResolver struct{ hosts map[string][]string }

func (r staticHostResolver) LookupHost(_ context.Context, name string) ([]string, error) {
	addresses, ok := r.hosts[name]
	if !ok {
		return nil, context.DeadlineExceeded
	}
	return append([]string(nil), addresses...), nil
}

func (staticHostResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }

// staticFeedResolver is an httpfeed.Resolver fixed to the host-address map it
// was constructed with.
type staticFeedResolver struct{ hosts map[string][]netip.Addr }

func (r staticFeedResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addresses, ok := r.hosts[host]
	if !ok {
		return nil, fmt.Errorf("no answer for %q", host)
	}
	return append([]netip.Addr(nil), addresses...), nil
}

func TestTurnsOneURLIntoASafeLocalServiceUsableByTheExistingRenderers(t *testing.T) {
	browser := discovery.DefaultBrowserPath()
	if browser == "" {
		t.Skip("set ROUTEVANE_BROWSER to a Chromium-family executable to run the discovery end-to-end test")
	}
	certificate, pin := generatePinnedCertificate(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>` +
				`<img src="https://static.shop.example.co.uk/logo.png" alt="">` +
				`<img src="https://cdn.thirdparty.test/pixel.png" alt="">` +
				`<img src="https://tracker.internal.test/pixel.png" alt="">` +
				`</body></html>`))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	address := server.Listener.Addr().String()

	feedHosts := map[string][]netip.Addr{
		"shop.example.co.uk":        {netip.MustParseAddr("203.0.113.30")},
		"www.shop.example.co.uk":    {netip.MustParseAddr("203.0.113.31")},
		"static.shop.example.co.uk": {netip.MustParseAddr("203.0.113.32")},
		"cdn.thirdparty.test":       {netip.MustParseAddr("203.0.113.40")},
		"tracker.internal.test":     {netip.MustParseAddr("192.168.4.4")},
	}
	rules := make([]string, 0, len(feedHosts))
	for host := range feedHosts {
		rules = append(rules, "MAP "+host+" "+address)
	}
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{
		Resolver:             staticHostResolver{hosts: map[string][]string{"shop.example.co.uk": {"203.0.113.30"}, "static.shop.example.co.uk": {"203.0.113.32"}}},
		FeedResolver:         staticFeedResolver{hosts: feedHosts},
		DiscoveryDialer:      redirectDialer{target: address},
		DiscoveryHostRules:   strings.Join(rules, ","),
		DiscoveryTrustedSPKI: []string{pin},
		DiscoveryBrowserPath: browser,
		Now:                  func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) },
		Context:              context.Background(),
	}

	// The user sees the exact URL first. Nothing is launched and nothing is
	// written by an unconfirmed run.
	preview := discoverViaCLI(t, deps, []string{"discover", "--url", "shop.example.co.uk", "--catalog-dir", catalogDir, "--data-dir", dataDir})
	if preview.URL != "https://shop.example.co.uk/" || preview.Confirmed {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.RegistrableDomain != "example.co.uk" || preview.PublicSuffix != "co.uk" || !preview.ICANNSuffix {
		t.Fatalf("the public suffix list must resolve co.uk: %#v", preview)
	}
	if preview.ServiceID != "example" || preview.Hint == "" || preview.DraftPath != "" {
		t.Fatalf("preview = %#v", preview)
	}
	if _, err := os.Stat(filepath.Join(catalogDir, "local")); err == nil {
		t.Fatal("an unconfirmed run must not write anything")
	}

	confirmed := discoverViaCLI(t, deps, []string{"discover", "--url", "shop.example.co.uk", "--confirm", "--service-id", "shop", "--title", "Shop", "--catalog-dir", catalogDir, "--data-dir", dataDir})
	if !confirmed.Confirmed || confirmed.DraftPath == "" {
		t.Fatalf("confirmed = %#v", confirmed)
	}
	// Same-site hosts reach the draft; the third party and the host that resolves
	// into a private network do not.
	for _, host := range []string{"shop.example.co.uk", "static.shop.example.co.uk"} {
		if !containsString(confirmed.AcceptedHosts, host) {
			t.Fatalf("same-site host %q missing: %#v", host, confirmed.AcceptedHosts)
		}
	}
	if !containsString(confirmed.SeedDomains, "example.co.uk") {
		t.Fatalf("the registrable domain must be the routing seed: %#v", confirmed.SeedDomains)
	}
	for _, host := range []string{"cdn.thirdparty.test", "tracker.internal.test"} {
		if containsString(confirmed.AcceptedHosts, host) {
			t.Fatalf("host %q must not reach the draft: %#v", host, confirmed.AcceptedHosts)
		}
	}
	candidateReasons := map[string][]string{}
	for _, candidate := range confirmed.Candidates {
		candidateReasons[candidate.Host] = candidate.Reasons
	}
	if !containsString(candidateReasons["cdn.thirdparty.test"], discovery.CandidateThirdPartyDomain) {
		t.Fatalf("third party must stay a candidate: %#v", confirmed.Candidates)
	}
	if !containsString(candidateReasons["tracker.internal.test"], discovery.RefusedLocalDestination) {
		t.Fatalf("a host resolving into a private network must be refused: %#v", confirmed.Candidates)
	}
	if confirmed.Sightings == 0 {
		t.Fatalf("the DNS cycle recorded no observation: %#v", confirmed)
	}

	// The written draft contains domains only.
	payload, err := os.ReadFile(confirmed.DraftPath)
	if err != nil {
		t.Fatal(err)
	}
	draftText := string(payload)
	for _, forbidden := range []string{"203.0.113", "192.168", "/24", "/32", "cdn.thirdparty.test", "tracker.internal.test"} {
		if strings.Contains(draftText, forbidden) {
			t.Fatalf("draft contains %q:\n%s", forbidden, draftText)
		}
	}

	// The new service is immediately usable by an existing renderer.
	catalog, err := catalogyaml.Load(context.Background(), catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	definition, found := catalog.Service("shop")
	if !found {
		t.Fatalf("draft did not load: %#v", catalog.Services)
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	resource, err := domain.NewAddrResourceFromString("203.0.113.30")
	if err != nil {
		t.Fatal(err)
	}
	sighting := domain.Sighting{
		ServiceID: "shop", ComponentID: discovery.DefaultComponentID, Resource: resource,
		SourceID: definition.Sources[0].ID, SourceClass: domain.SourceObserved, SourceRevision: definition.Sources[0].Revision,
		ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid,
	}
	target := domain.TargetProfile{
		ID: "keenetic", ProfileKey: keenetic.Version, RendererID: keenetic.ID,
		Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize},
	}
	plan, err := planner.BuildPlanSet([]planner.ServiceInput{{Definition: definition, Sightings: []domain.Sighting{sighting}}}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := keenetic.Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := keenetic.Validate(artifact); err != nil {
		t.Fatalf("artifact from a discovered service is invalid: %v", err)
	}
	if !strings.Contains(string(artifact), "203.0.113.30") {
		t.Fatalf("artifact = %s", artifact)
	}

	// A second run never replaces the reviewed definition.
	repeat := &syncBuffer{}
	if code := runWithDeps(repeat, &syncBuffer{}, []string{"discover", "--url", "shop.example.co.uk", "--confirm", "--service-id", "shop", "--catalog-dir", catalogDir, "--data-dir", dataDir}, deps); code == 0 {
		t.Fatalf("a repeated discovery must not overwrite an existing definition: %s", repeat.String())
	}
}

func TestRefusesLocalAndUnusableTargetsWithoutStartingABrowser(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{Now: func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }, Context: context.Background()}
	cases := []string{
		"http://localhost:8080",
		"https://127.0.0.1",
		"https://169.254.169.254/latest",
		"https://10.1.2.3",
		"printer.local",
		"file:///etc/passwd",
		"https://co.uk",
	}
	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			stdout, stderr := &syncBuffer{}, &syncBuffer{}
			// No browser is configured, so reaching a page load at all would
			// fail with a different code than the target refusal below.
			code := runWithDeps(stdout, stderr, []string{"discover", "--url", value, "--confirm", "--catalog-dir", catalogDir, "--data-dir", dataDir}, deps)
			if code == 0 {
				t.Fatalf("target %q was accepted: %s", value, stdout.String())
			}
			if strings.Contains(stderr.String(), "page_load_failed") {
				t.Fatalf("target %q reached a page load: %s", value, stderr.String())
			}
		})
	}
}

func discoverViaCLI(t *testing.T, deps runtimeDeps, args []string) discoverReport {
	t.Helper()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("discover %v failed: code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
	}
	var report discoverReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
	return report
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func generatePinnedCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "routevane-service-from-url-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(publicKeyDER)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, base64.StdEncoding.EncodeToString(digest[:])
}
