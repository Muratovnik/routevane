//go:build darwin

package plugin

import (
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func holdPluginRunnerThread() func() { return func() {} }

func preparePluginRunnerLimits() error {
	for _, limit := range pluginResourceLimits() {
		value := unix.Rlimit{Cur: limit.value, Max: limit.value}
		if err := unix.Setrlimit(limit.resource, &value); err != nil {
			return err
		}
	}
	return nil
}

func startLimitedPlugin(command *exec.Cmd, _ <-chan os.Signal) error {
	return command.Start()
}
