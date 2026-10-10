package discovery

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type echoTunnelDialer struct {
	peer   chan net.Conn
	closed chan struct{}
}

func (d echoTunnelDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	connection, peer := net.Pipe()
	d.peer <- peer
	go func() {
		defer close(d.closed)
		defer peer.Close()
		_, _ = io.Copy(peer, peer)
	}()
	return connection, nil
}

func TestDiscoveryOwnsEstablishedTunnelsUntilTheyAreClosed(t *testing.T) {
	for _, stopKind := range []string{"explicit stop", "session cancellation"} {
		t.Run(stopKind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dialer := echoTunnelDialer{peer: make(chan net.Conn, 1), closed: make(chan struct{})}
			proxy := newGuardedProxy(fixtureResolver{"one.test": {netip.MustParseAddr("203.0.113.1")}}, dialer, 10, 10, 1024)
			address, stop, err := proxy.listenLoopback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			client, err := net.DialTimeout("tcp4", address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(client, "CONNECT one.test:443 HTTP/1.1\r\nHost: one.test:443\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(client)
			response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("CONNECT status=%d", response.StatusCode)
			}
			peer := <-dialer.peer
			defer peer.Close()
			if _, err := io.WriteString(client, "before"); err != nil {
				t.Fatal(err)
			}
			payload := make([]byte, len("before"))
			if _, err := io.ReadFull(reader, payload); err != nil || string(payload) != "before" {
				t.Fatalf("tunnel did not work before shutdown: %q %v", payload, err)
			}
			if stopKind == "explicit stop" {
				stop()
			} else {
				cancel()
			}
			select {
			case <-dialer.closed:
			case <-time.After(time.Second):
				t.Error("the upstream tunnel remained alive after session stop")
			}
			if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ReadByte(); err != io.EOF {
				t.Errorf("the client tunnel was not closed after session stop: %v", err)
			}
		})
	}
}

type bufferedTunnelResponse struct {
	header   http.Header
	status   int
	client   net.Conn
	buffered *bufio.ReadWriter
}

func (w *bufferedTunnelResponse) Header() http.Header               { return w.header }
func (w *bufferedTunnelResponse) WriteHeader(status int)            { w.status = status }
func (w *bufferedTunnelResponse) Write(payload []byte) (int, error) { return len(payload), nil }
func (w *bufferedTunnelResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.client, w.buffered, nil
}

func TestDiscoveryForwardsAlreadyBufferedTunnelBytesThroughTheLimit(t *testing.T) {
	const payload = "bytes-read-with-CONNECT"
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprint("limited=", limited), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			reader := bufio.NewReader(io.MultiReader(strings.NewReader(payload), server))
			if _, err := reader.Peek(len(payload)); err != nil || reader.Buffered() < len(payload) {
				t.Fatalf("fixture did not prebuffer tunnel input: %v", err)
			}
			dialer := echoTunnelDialer{peer: make(chan net.Conn, 1), closed: make(chan struct{})}
			limit := int64(1024)
			if limited {
				limit = int64(len(payload) - 1)
			}
			proxy := newGuardedProxy(fixtureResolver{"one.test": {netip.MustParseAddr("203.0.113.1")}}, dialer, 10, 10, limit)
			response := &bufferedTunnelResponse{header: make(http.Header), client: server, buffered: bufio.NewReadWriter(reader, bufio.NewWriter(io.Discard))}
			request := httptest.NewRequest(http.MethodConnect, "https://one.test:443", nil).WithContext(ctx)
			request.Host = "one.test:443"
			if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { defer close(done); proxy.ServeHTTP(response, request) }()
			peer := <-dialer.peer
			defer peer.Close()
			defer func() {
				cancel()
				_ = client.Close()
				_ = server.Close()
				_ = peer.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("tunnel handler did not stop during owned cleanup")
				}
			}()
			got := make([]byte, len(payload))
			n, err := io.ReadFull(client, got)
			if limited {
				if n != 0 || err != io.EOF {
					t.Fatalf("prebuffered input escaped the byte bound: n=%d err=%v", n, err)
				}
			} else if err != nil || string(got) != payload {
				t.Fatalf("prebuffered input was lost: %q %v", got, err)
			}
		})
	}
}
