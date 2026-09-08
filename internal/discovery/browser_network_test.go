package discovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestBrowserAllocatorRefusesAnExistingProfile(t *testing.T) {
	profile := t.TempDir()
	defaultProfile := filepath.Join(profile, "Default")
	if err := os.Mkdir(defaultProfile, 0o700); err != nil {
		t.Fatal(err)
	}
	preferences := filepath.Join(defaultProfile, "Preferences")
	const sentinel = `{"existing":"profile"}`
	if err := os.WriteFile(preferences, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel, err := newBrowserAllocator(t.Context(), BrowserOptions{}, profile, "127.0.0.1:1")
	if cancel != nil {
		cancel()
	}
	if err == nil || ctx != nil {
		t.Fatal("existing profile must not produce a browser allocator")
	}
	contents, err := os.ReadFile(preferences)
	if err != nil || string(contents) != sentinel {
		t.Fatalf("existing preferences changed: %q, %v", contents, err)
	}
}

// Exercise the actual browser transport: HTTP proxy assertions alone cannot
// detect WebRTC sending STUN outside the proxy. The HTTPS report proves that
// the page started ICE gathering, rather than passing because its script failed.
func TestDiscoveryBrowserBlocksNonProxiedWebRTC(t *testing.T) {
	for _, mode := range []string{"page", "scenario"} {
		t.Run(mode, func(t *testing.T) {
			browserPath(t)
			udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
			if err != nil {
				t.Fatal(err)
			}
			var packets, privateRequests, reports atomic.Int64
			done := make(chan struct{})
			go func() {
				defer close(done)
				buffer := make([]byte, 4096)
				for {
					if _, _, readErr := udp.ReadFromUDP(buffer); readErr != nil {
						return
					}
					packets.Add(1)
				}
			}()
			t.Cleanup(func() { _ = udp.Close(); <-done })
			privateHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				privateRequests.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(privateHTTP.Close)
			certificate, pin := selfSignedCertificate(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host == "private.test" {
					privateRequests.Add(1)
				}
				if r.URL.Path == "/report" && r.URL.Query().Get("rtc") == "started" {
					reports.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.URL.Path != "/" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = fmt.Fprintf(w, `<!doctype html><html><body>
<img src="https://private.test/pixel" alt="">
<script>
fetch(%q).catch(() => {});
window.pc = new RTCPeerConnection({iceServers:[{urls:'stun:127.0.0.1:%d'}]});
pc.createDataChannel('discovery-test');
pc.createOffer().then(o => pc.setLocalDescription(o)).then(() => fetch('/report?rtc=started'));
</script></body></html>`, privateHTTP.URL, udp.LocalAddr().(*net.UDPAddr).Port)
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
			server.StartTLS()
			t.Cleanup(server.Close)
			fixture := &pageFixture{server: server, pin: pin, hosts: map[string][]netip.Addr{
				"page.test":    {netip.MustParseAddr("203.0.113.10")},
				"private.test": {netip.MustParseAddr("127.0.0.1")},
			}}
			options := fixture.options(t)
			// Unknown names cannot leave the fixture through browser-side DNS.
			options.HostResolverRules = "MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"
			target, err := NormalizeTarget("https://page.test/")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "page" {
				page, loadErr := LoadPage(context.Background(), target, options)
				if loadErr != nil || page.CleanupError != "" || !containsHost(page.Hosts, "page.test") || page.Blocked["private.test"] != RefusedLocalDestination || page.Bytes == 0 {
					t.Fatalf("page=%+v error=%v", page, loadErr)
				}
			} else {
				scenario := Scenario{Target: target.URL, Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: target.URL, SettleSeconds: 2}}}
				evidence, runErr := RunScenario(context.Background(), scenario, options)
				if runErr != nil || evidence.CleanupError != "" || evidence.Bytes == 0 {
					t.Fatalf("evidence=%+v error=%v", evidence, runErr)
				}
				byHost := map[string]HostEvidence{}
				for _, host := range evidence.Hosts {
					byHost[host.Host] = host
				}
				if byHost["page.test"].Requests == 0 || !containsHost(byHost["page.test"].StepIDs, "open") || byHost["private.test"].Refused != RefusedLocalDestination {
					t.Fatalf("hosts=%+v", evidence.Hosts)
				}
			}
			_ = udp.Close()
			<-done
			if reports.Load() == 0 {
				t.Fatal("page did not report successful WebRTC ICE setup over HTTPS")
			}
			if packets.Load() != 0 || privateRequests.Load() != 0 {
				t.Fatalf("browser bypassed destination policy: UDP packets=%d private HTTP requests=%d", packets.Load(), privateRequests.Load())
			}
			entries, err := os.ReadDir(options.UserDataParent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("profiles after session=%v error=%v", entries, err)
			}
		})
	}
}

func TestDiscoveryBrowserCleansUpAfterActiveCancellationAndDeadline(t *testing.T) {
	for _, mode := range []string{"page", "scenario"} {
		for _, stop := range []string{"cancel", "deadline"} {
			t.Run(mode+"/"+stop, func(t *testing.T) {
				browserPath(t)
				started := make(chan struct{}, 1)
				certificate, pin := selfSignedCertificate(t)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					select {
					case started <- struct{}{}:
					default:
					}
					<-r.Context().Done()
				}))
				server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
				server.StartTLS()
				t.Cleanup(server.Close)
				fixture := &pageFixture{server: server, pin: pin, hosts: map[string][]netip.Addr{"page.test": {netip.MustParseAddr("203.0.113.10")}}}
				options := fixture.options(t)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					if mode == "page" {
						target, _ := NormalizeTarget("https://page.test/")
						_, err := LoadPage(ctx, target, options)
						done <- err
					} else {
						_, err := RunScenario(ctx, Scenario{Target: "https://page.test/", Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: "https://page.test/", SettleSeconds: 1}}}, options)
						done <- err
					}
				}()
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("browser did not start the HTTPS request before deadline")
				}
				if stop == "cancel" {
					cancel()
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("interrupted navigation must fail")
					}
				case <-time.After(10 * time.Second):
					t.Fatal("interrupted browser did not stop")
				}
				entries, err := os.ReadDir(options.UserDataParent)
				if err != nil || len(entries) != 0 {
					t.Fatalf("profiles after interruption=%v error=%v", entries, err)
				}
			})
		}
	}
}
