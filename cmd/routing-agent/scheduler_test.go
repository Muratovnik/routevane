package main

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/processlock"
)

type testDelayTicker struct{ ch chan time.Time }

func (t *testDelayTicker) Chan() <-chan time.Time { return t.ch }
func (*testDelayTicker) Stop()                    {}

type serialResolver struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
}

func (r *serialResolver) LookupHost(context.Context, string) ([]string, error) {
	r.mu.Lock()
	r.calls++
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()
	r.mu.Lock()
	r.active--
	r.mu.Unlock()
	return []string{"192.0.2.1"}, nil
}

func (*serialResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }

func TestSchedulerRunsImmediateSerialCyclesAndCancelsDuringDelay(t *testing.T) {
	catalogRoot := writeExampleCatalog(t, exampleServiceYAML)
	dataRoot := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	resolver := &serialResolver{}
	tickerCalls := 0
	deps := runtimeDeps{
		Resolver: resolver,
		Now:      func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) },
		Context:  ctx,
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return parent, func() {}
		},
		NewDelayTicker: func(time.Duration) delayTicker {
			tickerCalls++
			ticker := &testDelayTicker{ch: make(chan time.Time, 1)}
			if tickerCalls == 1 {
				ticker.ch <- time.Now()
			} else {
				cancel()
			}
			return ticker
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(&stdout, &stderr, []string{"run", "--target", "raw-json", "--service", "example", "--interval", "1s", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, deps)
	if code != 0 {
		t.Fatalf("run code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	resolver.mu.Lock()
	calls, maxActive := resolver.calls, resolver.maxActive
	resolver.mu.Unlock()
	if calls != 2 || maxActive != 1 || tickerCalls != 2 {
		t.Fatalf("calls=%d maxActive=%d delays=%d", calls, maxActive, tickerCalls)
	}
}

func TestSchedulerRefusesLockContentionBeforeDNS(t *testing.T) {
	catalogRoot := writeExampleCatalog(t, exampleServiceYAML)
	dataRoot, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := processlock.Acquire(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	resolver := &serialResolver{}
	deps := runtimeDeps{Resolver: resolver, Now: func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(&stdout, &stderr, []string{"run", "--target", "raw-json", "--service", "example", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, deps)
	if code != 1 {
		t.Fatalf("run code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	resolver.mu.Lock()
	calls := resolver.calls
	resolver.mu.Unlock()
	if calls != 0 {
		t.Fatalf("contending scheduler called DNS %d times", calls)
	}
}
