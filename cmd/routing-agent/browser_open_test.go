package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserWaitsForEmbeddedUI(t *testing.T) {
	var ready atomic.Bool
	var calls atomic.Int32
	first := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			select {
			case first <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Routevane-UI-Digest", "sha256-"+strings.Repeat("a", 64))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- openBrowserWhenReady(ctx, server.URL, func(_ context.Context, origin string) error {
			if origin != server.URL || !ready.Load() {
				return errors.New("opened before readiness")
			}
			calls.Add(1)
			return nil
		})
	}()
	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal("no readiness probe")
	}
	if calls.Load() != 0 {
		t.Fatal("opened an unavailable UI")
	}
	ready.Store(true)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("browser must open exactly once")
	}
}

func TestBrowserServeLifecycleSurvivesOpenerFailure(t *testing.T) {
	opened := make(chan string, 2)
	deps := runtimeDeps{OpenBrowser: func(_ context.Context, origin string) error {
		opened <- origin
		return errors.New("test opener unavailable")
	}}
	origin, cancel, done, stderr := startServeServer(t, writeAlphaBetaCatalog(t), t.TempDir(), deps, "--open-browser")
	defer func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("serve exit=%d: %s", code, stderr.String())
			}
		case <-time.After(6 * time.Second):
			t.Error("serve or its browser worker did not stop")
		}
	}()
	response, err := http.Get(origin + "/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Header.Get("X-Routevane-UI-Digest") == "" {
		t.Skip("API-only checkout; canonical check builds the embedded UI")
	}
	select {
	case got := <-opened:
		if got != origin {
			t.Fatalf("opener received %q, want %q", got, origin)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ready UI was not passed to the opener")
	}
	response, err = http.Get(origin + "/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("opener failure stopped the service")
	}
	select {
	case <-opened:
		t.Fatal("opener was called more than once")
	default:
	}
}

func TestBrowserRefusesNonlocalOriginsAndAPIOnlyPages(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1:80", "http://example.com:80", "http://user@127.0.0.1:80", "http://127.0.0.1:80/?secret=x", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:80?", "http://127.0.0.1"} {
		if localBrowserOrigin(origin) {
			t.Fatalf("accepted %s", origin)
		}
	}
	var redirects atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirects.Add(1) }))
	defer target.Close()
	for _, status := range []int{http.StatusOK, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			var opened atomic.Bool
			err := openBrowserWhenReady(ctx, server.URL, func(context.Context, string) error { opened.Store(true); return nil })
			if !errors.Is(err, context.DeadlineExceeded) || opened.Load() {
				t.Fatalf("err=%v opened=%v", err, opened.Load())
			}
		})
	}
	if redirects.Load() != 0 {
		t.Fatal("readiness followed a redirect")
	}
	options, ok := parseServe([]string{"--open-browser"})
	if !ok || !options.OpenBrowser {
		t.Fatal("launcher flag rejected")
	}
}
