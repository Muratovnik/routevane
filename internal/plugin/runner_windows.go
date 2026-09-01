//go:build windows

package plugin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func runContainedExecutable(executable, directory string) int {
	// #nosec G204 -- executable is the private, checksum-verified snapshot
	// supplied by the parent Routevane process over its reserved runner mode.
	command := exec.Command(executable)
	command.Dir = directory
	command.Env = minimalEnvironment()
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "plugin-runner execute: %v\n", err)
		return 126
	}
	return 0
}
