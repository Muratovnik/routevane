package discovery

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

type pendingDial struct {
	address string
	result  chan error
}

type controlledDialer struct{ calls chan pendingDial }

func (d controlledDialer) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	call := pendingDial{address: address, result: make(chan error, 1)}
	select {
	case d.calls <- call:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case err := <-call.result:
		if err != nil {
			return nil, err
		}
		connection, peer := net.Pipe()
		_ = peer.Close()
		return connection, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func startConnect(ctx context.Context, proxy *guardedProxy, host string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodConnect, "https://"+host, nil).WithContext(ctx)
		request.Host = host
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		done <- response
	}()
	return done
}

func awaitDial(t *testing.T, ctx context.Context, dialer controlledDialer, done <-chan *httptest.ResponseRecorder) pendingDial {
	t.Helper()
	select {
	case call := <-dialer.calls:
		return call
	case response := <-done:
		t.Fatalf("CONNECT refused before dial: %d %s", response.Code, response.Body.String())
	case <-ctx.Done():
		t.Fatal("CONNECT did not reach controlled dial")
	}
	return pendingDial{}
}

func awaitConnect(t *testing.T, ctx context.Context, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-done:
		return response
	case <-ctx.Done():
		t.Fatal("CONNECT did not finish")
	}
	return nil
}

func requireHostLimit(t *testing.T, ctx context.Context, proxy *guardedProxy, dialer controlledDialer, host string) {
	t.Helper()
	done := startConnect(ctx, proxy, host+":443")
	select {
	case call := <-dialer.calls:
		call.result <- errors.New("unexpected connection")
		awaitConnect(t, ctx, done)
		t.Fatalf("host limit bypassed: dial to %s", call.address)
	case response := <-done:
		_, blocked := proxy.observed()
		if response.Code != http.StatusForbidden || blocked[host] != RefusedHostLimit {
			t.Fatalf("response=%d blocked=%v", response.Code, blocked)
		}
	case <-ctx.Done():
		t.Fatal("host limit admission did not finish")
	}
}

func TestProxyReservesDistinctHostsBeforeDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	dialer := controlledDialer{calls: make(chan pendingDial)}
	resolver := fixtureResolver{
		"one.test":   {netip.MustParseAddr("203.0.113.1")},
		"two.test":   {netip.MustParseAddr("203.0.113.2")},
		"three.test": {netip.MustParseAddr("203.0.113.3")},
	}
	proxy := newGuardedProxy(resolver, dialer, 50, 2, 1024)
	oneDone := startConnect(ctx, proxy, "one.test:443")
	one := awaitDial(t, ctx, dialer, oneDone)
	twoDone := startConnect(ctx, proxy, "two.test:443")
	two := awaitDial(t, ctx, dialer, twoDone)
	requireHostLimit(t, ctx, proxy, dialer, "three.test")
	one.result <- nil
	awaitConnect(t, ctx, oneDone)
	two.result <- nil
	awaitConnect(t, ctx, twoDone)
	hosts, _ := proxy.observed()
	if len(hosts) != 2 {
		t.Fatalf("contacted hosts=%v", hosts)
	}
	requireHostLimit(t, ctx, proxy, dialer, "three.test")
}

func TestProxySharesReservationsForNormalizedHosts(t *testing.T) {
	for _, test := range []struct{ first, repeat, host string }{
		{"ONE.TEST.:443", "one.test:8443", "one.test"},
		{"[2001:4860:4860:0:0:0:0:8888]:443", "[2001:4860:4860::8888]:443", "2001:4860:4860::8888"},
		{"[::ffff:8.8.8.8]:443", "8.8.8.8:443", "8.8.8.8"},
	} {
		t.Run(test.host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			dialer := controlledDialer{calls: make(chan pendingDial)}
			proxy := newGuardedProxy(fixtureResolver{"one.test": {netip.MustParseAddr("203.0.113.1")}}, dialer, 50, 1, 1024)
			firstDone := startConnect(ctx, proxy, test.first)
			first := awaitDial(t, ctx, dialer, firstDone)
			repeatDone := startConnect(ctx, proxy, test.repeat)
			repeat := awaitDial(t, ctx, dialer, repeatDone)
			first.result <- errors.New("first dial failed")
			awaitConnect(t, ctx, firstDone)
			// One failed attempt must not release the other attempt's slot.
			requireHostLimit(t, ctx, proxy, dialer, "two.test")
			repeat.result <- nil
			awaitConnect(t, ctx, repeatDone)
			hosts, _ := proxy.observed()
			if len(hosts) != 1 || hosts[0] != test.host {
				t.Fatalf("contacted hosts=%v", hosts)
			}
			// A recorded host can reconnect after capacity is exhausted.
			againDone := startConnect(ctx, proxy, test.repeat)
			again := awaitDial(t, ctx, dialer, againDone)
			again.result <- nil
			awaitConnect(t, ctx, againDone)
		})
	}
}

func TestProxyReleasesFailedAndCancelledReservations(t *testing.T) {
	for _, failure := range []string{"dial", "cancel", "resolve", "local"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			dialer := controlledDialer{calls: make(chan pendingDial)}
			resolver := fixtureResolver{"one.test": {netip.MustParseAddr("203.0.113.1")}, "two.test": {netip.MustParseAddr("203.0.113.2")}}
			if failure == "resolve" {
				delete(resolver, "one.test")
			}
			if failure == "local" {
				resolver["one.test"] = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
			}
			proxy := newGuardedProxy(resolver, dialer, 50, 1, 1024)
			requestCtx, cancelRequest := context.WithCancel(ctx)
			defer cancelRequest()
			firstDone := startConnect(requestCtx, proxy, "one.test:443")
			if failure == "dial" || failure == "cancel" {
				first := awaitDial(t, ctx, dialer, firstDone)
				if failure == "cancel" {
					cancelRequest()
				} else {
					first.result <- errors.New("dial failed")
				}
			}
			response := awaitConnect(t, ctx, firstDone)
			if response.Code != http.StatusForbidden && response.Code != http.StatusBadGateway {
				t.Fatalf("failed request=%d", response.Code)
			}
			hosts, _ := proxy.observed()
			if len(hosts) != 0 {
				t.Fatalf("failed attempt counted as contacted: %v", hosts)
			}
			nextDone := startConnect(ctx, proxy, "two.test:443")
			next := awaitDial(t, ctx, dialer, nextDone)
			next.result <- nil
			awaitConnect(t, ctx, nextDone)
			hosts, _ = proxy.observed()
			if len(hosts) != 1 || hosts[0] != "two.test" {
				t.Fatalf("released reservation did not admit next host: %v", hosts)
			}
		})
	}
}
