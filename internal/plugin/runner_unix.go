//go:build linux || darwin

package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	pluginAddressSpaceLimit = 2 << 30
	pluginCPUSecondsLimit   = 300
	pluginOpenFilesLimit    = 128
)

func runContainedExecutable(executable, directory string) int {
	if unix.Getpgrp() != os.Getpid() {
		fmt.Fprintln(os.Stderr, "plugin-runner does not own its process group")
		return 126
	}
	if err := os.Chdir(directory); err != nil {
		fmt.Fprintf(os.Stderr, "plugin-runner working directory: %v\n", err)
		return 126
	}
	limits := []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_AS, pluginAddressSpaceLimit},
		{unix.RLIMIT_CPU, pluginCPUSecondsLimit},
		{unix.RLIMIT_NOFILE, pluginOpenFilesLimit},
		{unix.RLIMIT_CORE, 0},
	}
	for _, limit := range limits {
		value := unix.Rlimit{Cur: limit.value, Max: limit.value}
		if err := unix.Setrlimit(limit.resource, &value); err != nil {
			fmt.Fprintf(os.Stderr, "plugin-runner resource limit: %v\n", err)
			return 126
		}
	}
	// Keep the trusted runner alive as group leader while plugin code runs.
	// Its identity remains owned until all of its descendants are terminated.
	termination := make(chan os.Signal, 1)
	signal.Notify(termination, syscall.SIGTERM)
	defer signal.Stop(termination)
	// #nosec G204 -- executable is the checksum-verified snapshot supplied by
	// the private parent-owned runner protocol; no shell interprets this path.
	command := exec.Command(executable)
	command.Dir = directory
	command.Env = minimalEnvironment()
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "plugin-runner execute: %v\n", err)
		return 126
	}
	finished := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-termination:
	}
	// The runner still owns this PID and group. Kill includes the runner,
	// so a recycled numeric group is never signalled after it has been reaped.
	if err := unix.Kill(-os.Getpid(), unix.SIGKILL); err != nil {
		fmt.Fprintf(os.Stderr, "plugin-runner terminate process group: %v\n", err)
		return 126
	}
	return 0
}
