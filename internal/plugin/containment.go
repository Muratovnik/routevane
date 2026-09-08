package plugin

import (
	"context"
	"os/exec"
)

// processContainment installs platform resource limits before the private
// runner is allowed to execute plugin bytes.
type processContainment interface {
	prepare(*exec.Cmd) error
	attach(*exec.Cmd) error
	terminate(*exec.Cmd) error
	// waitEmpty confirms all contained processes exited before their files are removed.
	waitEmpty(context.Context) error
	close() error
}
