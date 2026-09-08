package discovery

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/netpolicy"
)

// Resolver resolves an observed host to its candidate addresses. The interface
// is declared here because the proxy is the consumer that must inspect every
// candidate before a connection is opened on the browser's behalf.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer opens a connection to a destination the policy has already accepted.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Refusal reason codes. They are stable identities, reported in diagnostics.
const (
	RefusedLocalDestination = "local_destination"
	RefusedPlaintext        = "plaintext_request"
	RefusedUnresolvable     = "unresolvable_host"
	RefusedRequestLimit     = "request_limit"
	RefusedHostLimit        = "host_limit"
	RefusedByteLimit        = "byte_limit"
	RefusedInvalidHost      = "invalid_host"
	// RefusedBrowserBackgroundService is a host the browser itself contacts for
	// updates, sign-in, or telemetry. It describes the browser, not the site.
	RefusedBrowserBackgroundService = "browser_background_service"
)

// guardedProxy checks destinations for the discovery browser's HTTP tunnels.
// The browser has no proxy bypass list; its separate WebRTC transport also
// disables non-proxied UDP through the isolated profile's routing preference.
//
// Plaintext requests are refused rather than forwarded: the session starts from
// an HTTPS URL, so a plaintext subresource is either downgraded or unrelated,
// and forwarding it would widen the boundary for no discovery value.
type guardedProxy struct {
	resolver    Resolver
	dialer      Dialer
	maxRequests int
	maxHosts    int
	maxBytes    int64

	bytes    atomic.Int64
	requests atomic.Int64

	mu    sync.Mutex
	hosts map[string]int
	// pending counts in-flight attempts for hosts not yet contacted. Together
	// with hosts it reserves unique destination capacity before DNS or dial.
	pending map[string]int
	blocked map[string]string
	// step and stepComponent describe the exploration step that is running, so a
	// host can be attributed to an action the user actually performed.
	step          string
	stepComponent string
	stepDocument  string
	steps         map[string]map[string]struct{}
	components    map[string]string
	loaders       map[string]map[string]struct{}
}

func newGuardedProxy(resolver Resolver, dialer Dialer, maxRequests, maxHosts int, maxBytes int64) *guardedProxy {
	return &guardedProxy{
		resolver:    resolver,
		dialer:      dialer,
		maxRequests: maxRequests,
		maxHosts:    maxHosts,
		maxBytes:    maxBytes,
		hosts:       make(map[string]int),
		pending:     make(map[string]int),
		blocked:     make(map[string]string),
		steps:       make(map[string]map[string]struct{}),
		components:  make(map[string]string),
		loaders:     make(map[string]map[string]struct{}),
	}
}

// beginStep records which exploration step is running. Every host contacted from
// now on is attributed to it until the next call.
func (p *guardedProxy) beginStep(stepID, component, documentHost string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.step, p.stepComponent = stepID, component
	if documentHost != "" {
		p.stepDocument = documentHost
	}
}

func (p *guardedProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		host := requestHost(r)
		p.refuse(host, RefusedPlaintext)
		http.Error(w, "plaintext requests are refused during discovery", http.StatusForbidden)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		p.refuse(r.Host, RefusedInvalidHost)
		http.Error(w, "invalid destination", http.StatusBadRequest)
		return
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		host = address.Unmap().String()
	} else {
		normalized, normalizeErr := domain.NormalizeDomain(host)
		if normalizeErr != nil {
			p.refuse(strings.ToLower(host), RefusedInvalidHost)
			http.Error(w, "destination refused by discovery policy", http.StatusForbidden)
			return
		}
		host = normalized
	}
	if reason := p.attempt(host); reason != "" {
		http.Error(w, "destination refused by discovery policy", http.StatusForbidden)
		return
	}
	defer p.releaseAttempt(host)
	address, reason := p.resolveAllowed(r.Context(), host, port)
	if reason != "" {
		p.refuse(host, reason)
		http.Error(w, "destination refused by discovery policy", http.StatusForbidden)
		return
	}
	upstream, err := p.dialer.DialContext(r.Context(), "tcp", address)
	if err != nil {
		p.refuse(host, RefusedUnresolvable)
		http.Error(w, "destination unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	// A host counts as contacted only once the policy has accepted it and a
	// connection exists. A refused host is reported as refused, never as
	// evidence about the page.
	p.connected(host)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "tunnel unsupported", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if buffered != nil {
		if err := buffered.Flush(); err != nil {
			return
		}
	}
	p.tunnel(client, upstream)
}

// tunnel copies both directions and stops the whole session once the byte
// budget is spent, so a discovery run cannot become an unbounded download.
func (p *guardedProxy) tunnel(client, upstream net.Conn) {
	var group sync.WaitGroup
	group.Add(2)
	copyBounded := func(destination io.Writer, source io.Reader, closer net.Conn) {
		defer group.Done()
		buffer := make([]byte, 32<<10)
		for {
			read, readErr := source.Read(buffer)
			if read > 0 {
				if p.bytes.Add(int64(read)) > p.maxBytes {
					p.refuse("", RefusedByteLimit)
					_ = closer.Close()
					return
				}
				if _, writeErr := destination.Write(buffer[:read]); writeErr != nil {
					_ = closer.Close()
					return
				}
			}
			if readErr != nil {
				_ = closer.Close()
				return
			}
		}
	}
	go copyBounded(upstream, client, upstream)
	go copyBounded(client, upstream, client)
	group.Wait()
}

// attempt applies the session bounds to one normalized host. An empty return
// value reserves capacity until connected or releaseAttempt finishes the attempt.
func (p *guardedProxy) attempt(host string) string {
	if backgroundService(host) {
		// The browser's own background services are not evidence about the page.
		// This list describes the browser, not the site, so it never affects
		// which of the site's dependencies may be activated.
		p.refuse(host, RefusedBrowserBackgroundService)
		return RefusedBrowserBackgroundService
	}
	if p.requests.Add(1) > int64(p.maxRequests) {
		p.refuse(host, RefusedRequestLimit)
		return RefusedRequestLimit
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, known := p.hosts[host]; !known {
		if p.pending[host] == 0 && len(p.hosts)+len(p.pending) >= p.maxHosts {
			p.blocked[host] = RefusedHostLimit
			return RefusedHostLimit
		}
		p.pending[host]++
	}
	return ""
}

func (p *guardedProxy) releaseAttempt(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending[host] <= 1 {
		delete(p.pending, host)
	} else {
		p.pending[host]--
	}
}

// connected records a host the policy accepted and a connection reached,
// together with the exploration step that was running.
func (p *guardedProxy) connected(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hosts[host]++
	delete(p.pending, host)
	delete(p.blocked, host)
	if p.step != "" {
		if _, known := p.steps[host]; !known {
			p.steps[host] = map[string]struct{}{}
		}
		p.steps[host][p.step] = struct{}{}
		p.components[host] = strongerComponent(p.components[host], p.stepComponent)
	}
	if p.stepDocument != "" && p.stepDocument != host {
		if _, known := p.loaders[host]; !known {
			p.loaders[host] = map[string]struct{}{}
		}
		p.loaders[host][p.stepDocument] = struct{}{}
	}
}

// evidence returns the session's observations in the shared evidence shape.
func (p *guardedProxy) evidence(target Target) SessionEvidence {
	p.mu.Lock()
	defer p.mu.Unlock()
	hosts := make([]HostEvidence, 0, len(p.hosts)+len(p.blocked))
	steps := map[string]struct{}{}
	for host, requests := range p.hosts {
		evidence := HostEvidence{Host: host, Requests: requests, Component: p.components[host]}
		evidence.StepIDs = slices.Sorted(maps.Keys(p.steps[host]))
		for _, step := range evidence.StepIDs {
			steps[step] = struct{}{}
		}
		evidence.LoadedBy = slices.Sorted(maps.Keys(p.loaders[host]))
		hosts = append(hosts, evidence)
	}
	for host, reason := range p.blocked {
		if _, contacted := p.hosts[host]; contacted {
			continue
		}
		hosts = append(hosts, HostEvidence{Host: host, Refused: reason})
	}
	slices.SortFunc(hosts, func(a, b HostEvidence) int { return cmp.Compare(a.Host, b.Host) })
	return SessionEvidence{
		Target:   target,
		Steps:    slices.Sorted(maps.Keys(steps)),
		Hosts:    hosts,
		Requests: int(p.requests.Load()),
		Bytes:    p.bytes.Load(),
	}
}

// browserBackgroundServices are the hosts a Chromium build contacts for its own
// updates, sign-in, and telemetry. They are refused and excluded from the
// observed set because a discovery session must describe the site, not the
// browser. Suppressing them by flag alone is not reliable across builds.
var browserBackgroundServices = []string{
	"accounts.google.com",
	"clients2.google.com",
	"clientservices.googleapis.com",
	"content-autofill.googleapis.com",
	"optimizationguide-pa.googleapis.com",
	"safebrowsing.googleapis.com",
	"update.googleapis.com",
	"www.googleapis.com",
	"chromewebstore.googleapis.com",
	"gstatic.com",
	"www.gstatic.com",
	"edgedl.me.gvt1.com",
}

func backgroundService(host string) bool {
	for _, known := range browserBackgroundServices {
		if host == known {
			return true
		}
	}
	return false
}

func (p *guardedProxy) resolveAllowed(ctx context.Context, host, port string) (string, string) {
	if netpolicy.LocalHostname(host) {
		return "", RefusedLocalDestination
	}
	if literal, err := netip.ParseAddr(host); err == nil {
		if !netpolicy.PublicUnicast(literal) {
			return "", RefusedLocalDestination
		}
		return net.JoinHostPort(literal.Unmap().String(), port), ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	candidates, err := p.resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil {
		return "", RefusedUnresolvable
	}
	if !netpolicy.AllPublicUnicast(candidates) {
		return "", RefusedLocalDestination
	}
	return net.JoinHostPort(candidates[0].Unmap().String(), port), ""
}

func (p *guardedProxy) refuse(host, reason string) {
	if host == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, recorded := p.blocked[host]; !recorded {
		p.blocked[host] = reason
	}
}

// observed returns the hosts the browser actually contacted and the hosts the
// policy refused, both in a stable order.
func (p *guardedProxy) observed() ([]string, map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	hosts := slices.Sorted(maps.Keys(p.hosts))
	blocked := make(map[string]string, len(p.blocked))
	for host, reason := range p.blocked {
		blocked[host] = reason
	}
	return hosts, blocked
}

func requestHost(r *http.Request) string {
	if r.URL != nil && r.URL.Host != "" {
		return strings.ToLower(r.URL.Hostname())
	}
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(r.Host)
}

// listenLoopback starts the proxy on an ephemeral loopback port, so the
// listener never leaves the machine. It is not a client-authentication gate:
// it binds to loopback and lives only for the session's duration, but it does
// not verify that the browser is the process on the other end of any given
// CONNECT. Any local process running as the same OS user could dial it while
// the session is live. That is an accepted boundary of the discovery proxy,
// not an enforced one; see security-boundaries.md.
func (p *guardedProxy) listenLoopback(ctx context.Context) (string, func(), error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("start discovery proxy: %w", err)
	}
	server := &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return
		}
	}()
	stop := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = listener.Close()
		<-done
	}
	return listener.Addr().String(), stop, nil
}
