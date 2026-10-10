//go:build linux || darwin

package plugin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const unownedRunnerProbeDirectory = "ROUTEVANE_TEST_UNOWNED_RUNNER_DIR"

func TestRunnerRefusesAProcessGroupItDoesNotOwn(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n: > executed\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, executable, "-test.run=^TestUnownedRunnerProbe$")
	probe.Env = append(os.Environ(), unownedRunnerProbeDirectory+"="+directory)
	// The probe owns a separate group. Even a broken subject that signals its
	// inherited group cannot terminate this test runner or unrelated processes.
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if output, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("runner ownership probe: %v\n%s", err, output)
	}
}

func TestUnownedRunnerProbe(t *testing.T) {
	directory := os.Getenv(unownedRunnerProbeDirectory)
	if directory == "" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Deliberately inherit the probe's process group instead of making the
	// private runner its leader. It must refuse before executing plugin code.
	subject := exec.CommandContext(ctx, executable, processRunnerCommand, filepath.Join(directory, "target"), directory)
	subject.Stdin = bytes.NewReader([]byte{0xa7})
	output, err := subject.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 126 {
		t.Fatalf("unowned runner error = %v, output = %q", err, output)
	}
	if !strings.Contains(string(output), "does not own its process group") {
		t.Fatalf("unowned runner reason = %q", output)
	}
	if _, err := os.Stat(filepath.Join(directory, "executed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unowned runner executed plugin code: %v", err)
	}
}
