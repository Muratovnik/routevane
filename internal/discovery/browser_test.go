package discovery

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
	"errors"
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
)

// The discovery browser is an owned external dependency, exactly like the
// Playwright browsers the web gate uses. These tests run in the browser gate,
// which sets ROUTEVANE_BROWSER, and skip elsewhere so the default gate needs no
// browser binary.
func browserPath(t *testing.T) string {
	t.Helper()
	path := DefaultBrowserPath()
	if path == "" {
		t.Skip("set ROUTEVANE_BROWSER to a Chromium-family executable to run the discovery browser tests")
	}
	return path
}

type pageFixture struct {
	server *httptest.Server
	pin    string
	hosts  map[string][]netip.Addr
}

// newPageFixture serves one page whose subresources live on a same-site host, a
// third-party host, and a host that resolves into a private network.
func newPageFixture(t *testing.T) *pageFixture {
	t.Helper()
	certificate, pin := selfSignedCertificate(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>page</title>` +
				`<link rel="stylesheet" href="https://static.page.test/site.css">` +
				`</head><body>` +
				`<img src="https://cdn.thirdparty.test/pixel.png" alt="">` +
				`<img src="https://internal.thirdparty.test/pixel.png" alt="">` +
				`<script src="https://api.page.test/app.js"></script>` +
				`</body></html>`))
		case "/site.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body{color:#000}"))
		case "/app.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte("void 0;"))
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
		}
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return &pageFixture{
		server: server,
		pin:    pin,
		hosts: map[string][]netip.Addr{
			"page.test":                {netip.MustParseAddr("203.0.113.10")},
			"static.page.test":         {netip.MustParseAddr("203.0.113.11")},
			"api.page.test":            {netip.MustParseAddr("203.0.113.12")},
			"cdn.thirdparty.test":      {netip.MustParseAddr("203.0.113.20")},
			"internal.thirdparty.test": {netip.MustParseAddr("10.1.2.3")},
		},
	}
}

func (f *pageFixture) options(t *testing.T) BrowserOptions {
	t.Helper()
	address := f.server.Listener.Addr().String()
	rules := make([]string, 0, len(f.hosts))
	for host := range f.hosts {
		rules = append(rules, "MAP "+host+" "+address)
	}
	return BrowserOptions{
		ExecPath:          browserPath(t),
		UserDataParent:    t.TempDir(),
		Timeout:           60 * time.Second,
		Resolver:          fixtureResolver(f.hosts),
		Dialer:            loopbackFixtureDialer{target: address},
		HostResolverRules: strings.Join(rules, ","),
		TrustedSPKI:       []string{f.pin},
	}
}

type fixtureResolver map[string][]netip.Addr

func (r fixtureResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, fmt.Errorf("no fixture answer for %q", host)
	}
	return append([]netip.Addr(nil), addresses...), nil
}

type loopbackFixtureDialer struct{ target string }

func (d loopbackFixtureDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, d.target)
}

func TestLoadPageObservesSameSiteAndThirdPartyHostsAndRefusesTheLocalNetwork(t *testing.T) {
	fixture := newPageFixture(t)
	target, err := NormalizeTarget("https://page.test/")
	if err != nil {
		t.Fatal(err)
	}
	page, err := LoadPage(context.Background(), target, fixture.options(t))
	if err != nil {
		t.Fatalf("load = %v (hosts=%v blocked=%v)", err, page.Hosts, page.Blocked)
	}
	for _, host := range []string{"page.test", "static.page.test", "api.page.test", "cdn.thirdparty.test"} {
		if !containsHost(page.Hosts, host) {
			t.Fatalf("host %q was not observed: %v", host, page.Hosts)
		}
	}
	// The host that resolves into a private network is refused by the proxy, so
	// the browser never opened a connection to it.
	if reason := page.Blocked["internal.thirdparty.test"]; reason != RefusedLocalDestination {
		t.Fatalf("blocked = %#v", page.Blocked)
	}
	if containsHost(page.Hosts, "internal.thirdparty.test") {
		t.Fatalf("a refused host must not appear as contacted: %v", page.Hosts)
	}
	if page.Requests == 0 || page.Bytes == 0 {
		t.Fatalf("session recorded no traffic: %#v", page)
	}

	// The isolated temporary profile is removed on success.
	if _, err := os.Stat(page.ProfileDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary profile survived the session: %v", err)
	}
	if page.CleanupError != "" {
		t.Fatalf("a successful cleanup must not report an error: %q", page.CleanupError)
	}

	draft, err := BuildDraft(DraftRequest{Target: target, ListID: "page", Page: page})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"api.page.test", "page.test", "static.page.test"} {
		if !containsHost(draft.AcceptedHosts, host) {
			t.Fatalf("same-site host %q did not reach the draft: %v", host, draft.AcceptedHosts)
		}
	}
	for _, candidate := range draft.Candidates {
		if SameSite(target.RegistrableDomain, candidate.Host) {
			t.Fatalf("a same-site host was recorded as a candidate: %#v", candidate)
		}
	}
	if !containsHost(candidateHostsOf(draft), "cdn.thirdparty.test") {
		t.Fatalf("third-party host must stay a candidate: %#v", draft.Candidates)
	}
	if containsHost(draft.AcceptedHosts, "cdn.thirdparty.test") || containsHost(draft.AcceptedHosts, "internal.thirdparty.test") {
		t.Fatalf("a third party reached the draft: %v", draft.AcceptedHosts)
	}
}

func TestLoadPageRemovesItsProfileWhenTheSessionFails(t *testing.T) {
	fixture := newPageFixture(t)
	options := fixture.options(t)
	// No resolver answer exists for this host, so the proxy refuses it and the
	// navigation fails.
	target, err := NormalizeTarget("https://unmapped.test/")
	if err != nil {
		t.Fatal(err)
	}
	page, err := LoadPage(context.Background(), target, options)
	if err == nil {
		t.Fatalf("a page that cannot load must report an error: %#v", page)
	}
	if page.ProfileDir == "" {
		t.Fatal("a failed session must still report which profile it used")
	}
	if _, statErr := os.Stat(page.ProfileDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("temporary profile survived a failed session: %v", statErr)
	}
	entries, readErr := os.ReadDir(options.UserDataParent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed session left %d entries behind", len(entries))
	}
}

func TestLoadPageRemovesItsProfileWhenTheSessionIsCancelled(t *testing.T) {
	fixture := newPageFixture(t)
	options := fixture.options(t)
	target, err := NormalizeTarget("https://page.test/")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, err := LoadPage(ctx, target, options)
	if err == nil {
		t.Fatal("a cancelled session must report an error")
	}
	if page.ProfileDir != "" {
		if _, statErr := os.Stat(page.ProfileDir); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("temporary profile survived a cancelled session: %v", statErr)
		}
	}
	entries, readErr := os.ReadDir(options.UserDataParent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled session left %d entries behind", len(entries))
	}
}

func TestLoadPageRefusesAnUnconfiguredBrowser(t *testing.T) {
	target, err := NormalizeTarget("https://page.test/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPage(context.Background(), target, BrowserOptions{}); !errors.Is(err, ErrBrowserUnavailable) {
		t.Fatalf("err = %v, want ErrBrowserUnavailable", err)
	}
	missing := BrowserOptions{ExecPath: filepath.Join(t.TempDir(), "no-such-browser")}
	if _, err := LoadPage(context.Background(), target, missing); !errors.Is(err, ErrBrowserUnavailable) {
		t.Fatalf("err = %v, want ErrBrowserUnavailable", err)
	}
}

func containsHost(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func selfSignedCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "routevane-discovery-test"},
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
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return certificate, base64.StdEncoding.EncodeToString(digest[:])
}
