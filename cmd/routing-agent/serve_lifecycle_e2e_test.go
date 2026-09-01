package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// startServeServer raises the serve process and blocks until its /health
// endpoint answers, which is the one lifecycle every serve e2e test needs:
// listen on a free loopback port, launch runWithDeps in a goroutine, confirm
// the process actually called the listener factory with the expected
// default address, then poll /health until it answers 200 or a deadline
// elapses.
//
// deps carries whatever a scenario needs to vary (Resolver, Now, the feed
// seams, and so on); Context, Listen, and SignalContext belong to this shared
// lifecycle and are set here, overwriting whatever the caller left in them.
func startServeServer(t *testing.T, catalog, data string, deps runtimeDeps, extraArgs ...string) (string, context.CancelFunc, <-chan int, *syncBuffer) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)
	type listenCall struct{ network, address string }
	called := make(chan listenCall, 1)
	deps.Context = ctx
	deps.Listen = func(network, address string) (net.Listener, error) {
		called <- listenCall{network, address}
		return listener, nil
	}
	deps.SignalContext = func(parent context.Context) (context.Context, context.CancelFunc) { return parent, func() {} }
	go func() {
		args := append([]string{"serve", "--catalog-dir", catalog, "--data-dir", data}, extraArgs...)
		done <- runWithDeps(stdout, stderr, args, deps)
	}()
	select {
	case call := <-called:
		if call.network != "tcp4" || call.address != "127.0.0.1:8765" {
			cancel()
			t.Fatalf("listen arguments=%#v", call)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("serve did not call listener factory")
	}
	origin := "http://" + listener.Addr().String()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(origin + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				stop := func() {
					// Every serve E2E uses the process-wide default client. Close its
					// pooled test connections before asking the server to shut down so
					// a previous request cannot keep the lifecycle oracle alive until
					// the production five-second shutdown deadline.
					http.DefaultClient.CloseIdleConnections()
					cancel()
				}
				return origin, stop, done, stderr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatalf("server did not start: stdout=%s stderr=%s", stdout.String(), stderr.String())
	return "", nil, nil, nil
}
