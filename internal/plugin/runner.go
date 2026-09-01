package plugin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	processRunnerCommand = "__routevane_plugin_runner"
	processRunnerGate    = byte(0xa7)
)

// RunProcessRunner handles Routevane's private plugin-runner mode. The normal
// command entry point and the plugin package's test entry point call it before
// parsing public commands. The runner is the same Routevane binary: it blocks
// until the parent has installed OS containment, applies child-side limits,
// and then replaces itself with or starts the verified plugin snapshot.
func RunProcessRunner(args []string) (handled bool, exitCode int) {
	if len(args) == 0 || args[0] != processRunnerCommand {
		return false, 0
	}
	if len(args) != 3 || !filepath.IsAbs(args[1]) || !filepath.IsAbs(args[2]) {
		fmt.Fprintln(os.Stderr, "invalid private plugin-runner invocation")
		return true, 126
	}
	var gate [1]byte
	if _, err := io.ReadFull(os.Stdin, gate[:]); err != nil || gate[0] != processRunnerGate {
		fmt.Fprintln(os.Stderr, "plugin-runner gate was not opened")
		return true, 126
	}
	return true, runContainedExecutable(args[1], args[2])
}
