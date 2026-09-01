//go:build linux || darwin

package plugin

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	pluginAddressSpaceLimit = 2 << 30
	pluginCPUSecondsLimit   = 300
	pluginOpenFilesLimit    = 128
)

func runContainedExecutable(executable, directory string) int {
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
	if err := unix.Exec(executable, []string{executable}, minimalEnvironment()); err != nil {
		fmt.Fprintf(os.Stderr, "plugin-runner execute: %v\n", err)
		return 126
	}
	return 0
}
