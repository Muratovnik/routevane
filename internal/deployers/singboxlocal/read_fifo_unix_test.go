//go:build linux || darwin

package singboxlocal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"golang.org/x/sys/unix"
)

func TestADeclaredFIFOIsRefusedWithoutWaitingForAnotherProcess(t *testing.T) {
	for _, stage := range []string{"configuration", "rule set"} {
		t.Run(stage, func(t *testing.T) {
			base := newFixture(t, "rules.json")
			path := base.ruleSetPath
			if stage == "configuration" {
				path = base.configPath
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := unix.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			finished := make(chan struct{})
			// An unfixed blocking open must also be released on a failing run.
			// Only this test's FIFO is opened; no other process is involved.
			t.Cleanup(func() {
				select {
				case <-finished:
					return
				default:
				}
				peer, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
				if err != nil {
					t.Errorf("release the test-owned FIFO: %v", err)
					return
				}
				defer unix.Close(peer)
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("FIFO read did not return after its peer was opened")
				}
			})
			go func() {
				defer close(finished)
				if stage == "configuration" {
					_, err := New().Probe(ctx, base.connection)
					result <- err
					return
				}
				_, err := New().Backup(ctx, application.DeviceInfo{Interface: path}, base.connection)
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil {
					t.Error("a FIFO was accepted as a regular file")
				}
			case <-ctx.Done():
				t.Error("opening a declared FIFO blocked past the caller deadline")
			}
		})
	}
}
