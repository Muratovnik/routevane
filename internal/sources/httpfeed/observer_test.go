package httpfeed

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// publicHost is a documentation-range address. It is not local, not private,
// and never a real internet host, so it expresses "an allowed destination"
// without the test reaching the network.
const publicHost = "203.0.113.10"

type stubResolver struct {
	hosts map[string][]netip.Addr
	calls atomic.Int64
}

func (r *stubResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.calls.Add(1)
	addresses, ok := r.hosts[host]
	if !ok {
		return nil, fmt.Errorf("no stub answer for %q", host)
	}
	return append([]netip.Addr(nil), addresses...), nil
}

// loopbackDialer connects to the local test server no matter which address the
// policy accepted. The address policy is therefore the only thing under test;
// the test never depends on reaching the resolved address.
type loopbackDialer struct {
	target string
	calls  atomic.Int64
}

func (d *loopbackDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	d.calls.Add(1)
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, d.target)
}

type feedFixture struct {
	server   *httptest.Server
	resolver *stubResolver
	dialer   *loopbackDialer
	observer *Observer
}

func newFeedFixture(t *testing.T, hosts map[string][]netip.Addr, handler http.Handler, options Options) *feedFixture {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	options.RootCAs = pool
	resolver := &stubResolver{hosts: hosts}
	dialer := &loopbackDialer{target: server.Listener.Addr().String()}
	return &feedFixture{server: server, resolver: resolver, dialer: dialer, observer: NewObserver(resolver, dialer, options)}
}

func (f *feedFixture) observe(t *testing.T, ctx context.Context, url string, format domain.FeedFormat) (Result, error) {
	t.Helper()
	return f.observer.Observe(ctx, Query{ListID: "example", ComponentID: "web", SourceID: "feed", SourceRevision: "rev", URL: url, Format: format}, time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
}

func textHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	})
}

func publicHosts(names ...string) map[string][]netip.Addr {
	hosts := make(map[string][]netip.Addr, len(names))
	for _, name := range names {
		hosts[name] = []netip.Addr{netip.MustParseAddr(publicHost)}
	}
	return hosts
}

func TestFeedRecordsOfficialObservationsFromAPublicDestination(t *testing.T) {
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler("# comment\n192.0.2.0/24\n\n198.51.100.7\r\n; other comment\n"), Options{Validity: 2 * time.Hour})
	result, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sightings) != 2 || result.Skipped != 0 {
		t.Fatalf("result = %#v", result)
	}
	values := []string{result.Sightings[0].Resource.CanonicalValue(), result.Sightings[1].Resource.CanonicalValue()}
	if values[0] != "192.0.2.0/24" || values[1] != "198.51.100.7" {
		t.Fatalf("values = %v", values)
	}
	for _, sighting := range result.Sightings {
		if sighting.SourceClass != domain.SourceOfficial || sighting.TTLKnown || sighting.ObservationCount != 1 {
			t.Fatalf("sighting = %#v", sighting)
		}
		if !sighting.ValidUntil.Equal(sighting.LastSeen.Add(2 * time.Hour)) {
			t.Fatalf("validity window = %v..%v", sighting.LastSeen, sighting.ValidUntil)
		}
	}
}

func TestFeedRefusesLocalAndSpecialUseDestinations(t *testing.T) {
	cases := []struct {
		name    string
		address string
	}{
		{"loopback", "127.0.0.1"},
		{"loopback ipv6", "::1"},
		{"unspecified", "0.0.0.0"},
		{"private class a", "10.0.0.5"},
		{"private class b", "172.16.9.9"},
		{"private class c", "192.168.1.1"},
		{"unique local ipv6", "fd00::1"},
		{"link local", "169.254.10.10"},
		{"cloud metadata", "169.254.169.254"},
		{"link local ipv6", "fe80::1"},
		{"carrier grade nat", "100.64.0.1"},
		{"protocol assignments", "192.0.0.1"},
		{"benchmarking", "198.18.0.1"},
		{"multicast", "224.0.0.1"},
		{"nat64", "64:ff9b::c000:221"},
		{"6to4", "2002::1"},
		{"teredo", "2001:0:1::1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture(t, map[string][]netip.Addr{"feed.example.com": {netip.MustParseAddr(testCase.address)}}, textHandler("192.0.2.1\n"), Options{})
			_, err := fixture.observe(t, context.Background(), "https://feed.example.com/feed.txt", domain.FeedFormatText)
			if !errors.Is(err, ErrUnsafeDestination) {
				t.Fatalf("err = %v, want ErrUnsafeDestination", err)
			}
			if fixture.dialer.calls.Load() != 0 {
				t.Fatalf("dialer was called %d times for a refused destination", fixture.dialer.calls.Load())
			}
		})
	}
}

func TestFeedRefusesAMixedAnswerContainingALocalAddress(t *testing.T) {
	hosts := map[string][]netip.Addr{"feed.example.com": {netip.MustParseAddr(publicHost), netip.MustParseAddr("127.0.0.1")}}
	fixture := newFeedFixture(t, hosts, textHandler("192.0.2.1\n"), Options{})
	_, err := fixture.observe(t, context.Background(), "https://feed.example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("err = %v, want ErrUnsafeDestination", err)
	}
	if fixture.dialer.calls.Load() != 0 {
		t.Fatal("a split answer containing a local address must refuse the whole connection")
	}
}

func TestFeedRefusesARedirectIntoAPrivateNetwork(t *testing.T) {
	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.txt" {
			http.Redirect(w, r, "https://internal.example.com/feed.txt", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("192.0.2.1\n"))
	}
	hosts := map[string][]netip.Addr{
		"example.com":          {netip.MustParseAddr(publicHost)},
		"internal.example.com": {netip.MustParseAddr("10.1.2.3")},
	}
	fixture := newFeedFixture(t, hosts, handler, Options{})
	_, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("err = %v, want ErrUnsafeDestination", err)
	}
}

func TestFeedRefusesAPlaintextRedirect(t *testing.T) {
	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/feed.txt", http.StatusFound)
	}
	fixture := newFeedFixture(t, publicHosts("example.com"), handler, Options{})
	_, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err = %v, want ErrInvalidURL", err)
	}
}

func TestFeedRefusesARedirectLoop(t *testing.T) {
	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/feed.txt?hop", http.StatusFound)
	}
	fixture := newFeedFixture(t, publicHosts("example.com"), handler, Options{})
	_, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("err = %v, want ErrTooManyRedirects", err)
	}
}

func TestFeedAbortsAnOversizedResponse(t *testing.T) {
	oversized := strings.Repeat("192.0.2.1\n", (MaxBytes/10)+16)
	if len(oversized) <= MaxBytes {
		t.Fatalf("fixture is not oversized: %d", len(oversized))
	}
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler(oversized), Options{})
	_, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrFeedTooLarge) {
		t.Fatalf("err = %v, want ErrFeedTooLarge", err)
	}
}

func TestFeedRefusesMoreEntriesThanTheLimit(t *testing.T) {
	// Each entry is one distinct address so the limit, not deduplication, is
	// what refuses the feed.
	var builder strings.Builder
	for index := 0; index <= MaxEntries; index++ {
		fmt.Fprintf(&builder, "198.51.%d.%d\n", index/256, index%256)
	}
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler(builder.String()), Options{})
	_, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrTooManyEntries) {
		t.Fatalf("err = %v, want ErrTooManyEntries", err)
	}
}

func TestFeedHonorsTheCallerDeadline(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		_, _ = w.Write([]byte("192.0.2.1\n"))
	}
	fixture := newFeedFixture(t, publicHosts("example.com"), handler, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := fixture.observe(t, ctx, "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("deadline was not enforced: %v", elapsed)
	}
}

func TestFeedSkipsUnusableEntriesWithoutRecordingThem(t *testing.T) {
	body := strings.Join([]string{
		"192.0.2.1",
		"999.1.1.1",
		"192.0.2.0/33",
		"192.0.2.1/",
		"example.com",
		"192.0.2.1 192.0.2.2",
		"-leading-hyphen.example",
		"::ffff:192.0.2.9",
		"198.51.100.0/24",
	}, "\n")
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler(body), Options{})
	result, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if err != nil {
		t.Fatal(err)
	}
	// The IPv4-mapped entry normalizes to its IPv4 form rather than being
	// recorded as a separate IPv6 resource, and a name is a name: a feed that
	// publishes domains is the material this product prefers.
	//
	// "999.1.1.1" is a mistyped address, not a host, and its all-numeric final
	// label is what says so.
	want := []string{"192.0.2.1", "example.com", "192.0.2.9", "198.51.100.0/24"}
	if len(result.Sightings) != len(want) {
		t.Fatalf("recorded %d entries, want %d: %#v", len(result.Sightings), len(want), result.Sightings)
	}
	for index, expected := range want {
		if got := result.Sightings[index].Resource.CanonicalValue(); got != expected {
			t.Fatalf("entry %d = %q, want %q", index, got, expected)
		}
	}
	if result.Skipped != 5 {
		t.Fatalf("skipped = %d, want 5", result.Skipped)
	}
	for _, sighting := range result.Sightings {
		if !sighting.Resource.IsValid() {
			t.Fatalf("recorded an invalid resource: %#v", sighting.Resource)
		}
	}
}

func TestFeedDecodesTheJSONShapesOfficialFeedsPublish(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"bare array", `["192.0.2.0/24","198.51.100.7"]`, []string{"192.0.2.0/24", "198.51.100.7"}},
		{"prefix objects", `{"syncToken":"1","prefixes":[{"ip_prefix":"192.0.2.0/24","region":"eu"},{"ipv6_prefix":"2001:db8::/32"}]}`, []string{"192.0.2.0/24", "2001:db8::/32"}},
		{"string list", `{"prefixes":["203.0.113.5"]}`, []string{"203.0.113.5"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture(t, publicHosts("example.com"), textHandler(testCase.body), Options{})
			result, err := fixture.observe(t, context.Background(), "https://example.com/feed.json", domain.FeedFormatJSON)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Sightings) != len(testCase.want) {
				t.Fatalf("sightings = %#v", result.Sightings)
			}
			for index, expected := range testCase.want {
				if got := result.Sightings[index].Resource.CanonicalValue(); got != expected {
					t.Fatalf("entry %d = %q, want %q", index, got, expected)
				}
			}
		})
	}
}

func TestFeedRefusesAMalformedOrEmptyBody(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		format domain.FeedFormat
		want   error
	}{
		{"malformed json", "{not json", domain.FeedFormatJSON, ErrMalformedFeed},
		{"json without prefixes", `{"other":[1,2]}`, domain.FeedFormatJSON, ErrEmptyFeed},
		{"comments only", "# nothing here\n", domain.FeedFormatText, ErrEmptyFeed},
		{"unusable entries only", "999.1.1.1\n-bad-.example\n", domain.FeedFormatText, ErrEmptyFeed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture(t, publicHosts("example.com"), textHandler(testCase.body), Options{})
			_, err := fixture.observe(t, context.Background(), "https://example.com/feed", testCase.format)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestFeedRefusesANonOKStatus(t *testing.T) {
	var handler http.HandlerFunc = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	fixture := newFeedFixture(t, publicHosts("example.com"), handler, Options{})
	_, err := fixture.observe(t, context.Background(), "https://example.com/feed.txt", domain.FeedFormatText)
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("err = %v, want ErrUnexpectedStatus", err)
	}
}

func TestValidateURLAcceptsOnlyAnAbsoluteCredentialFreeHTTPSURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"https host", "https://feed.example.com/list.txt", nil},
		{"https public literal", "https://203.0.113.10/list.txt", nil},
		{"plaintext", "http://feed.example.com/list.txt", ErrInvalidURL},
		{"no scheme", "feed.example.com/list.txt", ErrInvalidURL},
		{"credentials", "https://user:secret@feed.example.com/list.txt", ErrInvalidURL},
		{"fragment", "https://feed.example.com/list.txt#part", ErrInvalidURL},
		{"empty", "", ErrInvalidURL},
		{"surrounding space", " https://feed.example.com/list.txt", ErrInvalidURL},
		{"file scheme", "file:///etc/passwd", ErrInvalidURL},
		{"loopback literal", "https://127.0.0.1/list.txt", ErrUnsafeDestination},
		{"metadata literal", "https://169.254.169.254/latest", ErrUnsafeDestination},
		{"private literal", "https://[fd00::1]/list.txt", ErrUnsafeDestination},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateURL(testCase.raw)
			if testCase.want == nil && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if testCase.want != nil && !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestFeedRefusesAnInvalidQuery(t *testing.T) {
	fixture := newFeedFixture(t, publicHosts("example.com"), textHandler("192.0.2.1\n"), Options{})
	observedAt := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		query Query
		want  error
	}{
		{"missing service", Query{ComponentID: "web", SourceID: "feed", SourceRevision: "rev", URL: "https://example.com/f", Format: domain.FeedFormatText}, ErrInvalidQuery},
		{"missing revision", Query{ListID: "example", ComponentID: "web", SourceID: "feed", URL: "https://example.com/f", Format: domain.FeedFormatText}, ErrInvalidQuery},
		{"unknown format", Query{ListID: "example", ComponentID: "web", SourceID: "feed", SourceRevision: "rev", URL: "https://example.com/f", Format: domain.FeedFormat("xml")}, ErrUnsupportedFormat},
		{"local url", Query{ListID: "example", ComponentID: "web", SourceID: "feed", SourceRevision: "rev", URL: "https://127.0.0.1/f", Format: domain.FeedFormatText}, ErrUnsafeDestination},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.observer.Observe(context.Background(), testCase.query, observedAt); !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
	if _, err := fixture.observer.Observe(context.Background(), Query{ListID: "example", ComponentID: "web", SourceID: "feed", SourceRevision: "rev", URL: "https://example.com/f", Format: domain.FeedFormatText}, time.Time{}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("a zero observation time must be refused")
	}
}

func FuzzDecodeNeverYieldsAnInvalidResource(f *testing.F) {
	f.Add("192.0.2.1\n198.51.100.0/24\n# comment\n")
	f.Add(`{"prefixes":[{"ip_prefix":"192.0.2.0/24"}]}`)
	f.Add("[\"2001:db8::/32\"]")
	f.Add("::ffff:192.0.2.9\n")
	f.Add("192.0.2.0/0\n")
	f.Fuzz(func(t *testing.T, body string) {
		for _, format := range []domain.FeedFormat{domain.FeedFormatText, domain.FeedFormatJSON} {
			entries, skipped, err := decode(format, []byte(body))
			if err != nil {
				if !errors.Is(err, ErrTooManyEntries) && !errors.Is(err, ErrMalformedFeed) {
					t.Fatalf("unexpected error for %s: %v", format, err)
				}
				continue
			}
			if skipped < 0 || len(entries) > MaxEntries {
				t.Fatalf("bounds violated: entries=%d skipped=%d", len(entries), skipped)
			}
			for _, entry := range entries {
				if !entry.IsValid() {
					t.Fatalf("decoded an invalid resource from %q: %#v", body, entry)
				}
				if entry.Kind == domain.ResourcePrefix && entry.Prefix != entry.Prefix.Masked() {
					t.Fatalf("decoded a non-canonical prefix: %v", entry.Prefix)
				}
			}
		}
	})
}
