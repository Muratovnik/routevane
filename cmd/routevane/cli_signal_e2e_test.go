package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/processlock"
)

const cliSignalDeviceVariable = "ROUTEVANE_TEST_CLI_SIGNAL_DEVICE"

// Run the command in its own process so an unhandled signal cannot be mistaken
// for cancellation of a context that only a test supplied.
func TestCLIDeploySignalProcess(t *testing.T) {
	address := os.Getenv(cliSignalDeviceVariable)
	if address == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI arguments")
	}
	deps := productionDeps()
	deps.DeviceDialer = redirectDialer{target: address}
	os.Exit(runWithDeps(os.Stdout, os.Stderr, os.Args[separator+1:], deps))
}

func TestDirectCLIDeploySignalsWaitForRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Process.Signal cannot deliver console Ctrl+C on Windows")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, interrupt := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(interrupt.String(), func(t *testing.T) {
			catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
			data := filepath.Join(t.TempDir(), "data")
			now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
			resolver := &hostAddressResolver{}
			resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
			origin, stop, served, serverStderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
			defer stop()
			profileID, outputID, _ := createProfileOutput(t, origin, "CLI signal recovery", "keenetic", "youtube")
			artifactID := refreshAndBuild(t, origin, profileID, outputID).Artifact.ID
			stop()
			if code := <-served; code != 0 {
				t.Fatalf("artifact server=%d %s", code, serverStderr.String())
			}

			device := newFakeKeeneticDevice()
			before := map[string]string{"192.0.2.99/255.255.255.255": deviceInterfaceName, "203.0.113.7/255.255.255.255": "ISP"}
			device.routes = maps.Clone(before)
			device.failAfterAdd = true
			device.waitForCallerCancel = true
			device.callerCancelled = make(chan struct{})
			device.rollbackStarted = make(chan struct{})
			releaseRollback := make(chan struct{})
			device.releaseRollback = releaseRollback
			deviceWrite := make(chan struct{}, 1)
			device.cancelApply = func() { deviceWrite <- struct{}{} }
			deviceServer := httptest.NewServer(device.handler())
			defer deviceServer.Close()
			defer func() {
				select {
				case <-releaseRollback:
				default:
					close(releaseRollback)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestCLIDeploySignalProcess$", "--",
				"deploy", "--artifact", artifactID, "--target", "keenetic", "--device", "http://192.168.1.1",
				"--user", deviceUser, "--interface", deviceInterfaceName, "--catalog-dir", catalog, "--data-dir", data, "--confirm")
			command.Env = append(os.Environ(), cliSignalDeviceVariable+"="+deviceServer.Listener.Addr().String(),
				PluginsDirVariable+"=", DevicePasswordVariable+"="+devicePassword)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			exited := make(chan struct{})
			go func() {
				done <- command.Wait()
				close(exited)
			}()
			defer func() {
				cancel()
				<-exited
			}()

			select {
			case <-deviceWrite:
				// The handler reports an actual partial write. Start has returned
				// on this goroutine before it accesses the initialized Process.
				if err := command.Process.Signal(interrupt); err != nil {
					t.Fatal(err)
				}
			case err := <-done:
				t.Fatalf("CLI did not reach the device write: %v stderr=%s", err, stderr.String())
			case <-ctx.Done():
				t.Fatal("CLI did not reach the device write before its deadline")
			}
			select {
			case <-device.callerCancelled:
			case err := <-done:
				t.Fatalf("CLI exited without canceling the active device request: %v stderr=%s", err, stderr.String())
			case <-ctx.Done():
				t.Fatal("CLI signal did not cancel the active device request")
			}
			select {
			case <-device.rollbackStarted:
			case err := <-done:
				t.Fatalf("CLI exited without recovering the partial device write: %v stderr=%s", err, stderr.String())
			case <-ctx.Done():
				t.Fatal("CLI did not begin rollback after its signal")
			}
			select {
			case err := <-done:
				t.Fatalf("CLI exited while recovery was still in progress: %v", err)
			default:
			}
			if lock, err := processlock.Acquire(data); !errors.Is(err, processlock.ErrLocked) {
				if lock != nil {
					_ = lock.Close()
				}
				t.Fatalf("CLI released data ownership before rollback finished: %v", err)
			}
			close(releaseRollback)
			select {
			case err := <-done:
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("interrupted deployment exit=%v stderr=%s", err, stderr.String())
				}
			case <-ctx.Done():
				t.Fatal("CLI did not finish after recovery")
			}
			var report deployReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("report=%s error=%v", stdout.String(), err)
			}
			if report.Applied || !report.RolledBack {
				t.Fatalf("interrupted deployment report=%#v", report)
			}
			if status := processlock.Inspect(data); !status.Healthy || status.Locked {
				t.Fatalf("CLI did not release ownership after recovery: %#v", status)
			}
			device.mu.Lock()
			after, partial, restores := maps.Clone(device.routes), maps.Clone(device.partialState), device.restores
			device.mu.Unlock()
			if maps.Equal(partial, before) || partial["192.0.2.10/255.255.255.255"] != deviceInterfaceName {
				t.Fatalf("fixture did not reach a partial write: %v", partial)
			}
			if !maps.Equal(after, before) || restores != 1 {
				t.Fatalf("signal left device changes behind: before=%v after=%v restores=%d", before, after, restores)
			}
		})
	}
}
