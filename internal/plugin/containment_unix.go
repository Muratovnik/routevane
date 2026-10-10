//go:build linux || darwin

package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type unixProcessContainment struct{}

func newProcessContainment() (processContainment, error) {
	return unixProcessContainment{}, nil
}

func (unixProcessContainment) prepare(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func (unixProcessContainment) attach(*exec.Cmd) error { return nil }

func (unixProcessContainment) terminate(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	// The live trusted runner owns group termination. Process.Signal uses
	// os.Process's wait/signal lifetime instead of a possibly recycled group ID.
	err := command.Process.Signal(syscall.SIGTERM)
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func (unixProcessContainment) close() error { return nil }

// The Unix runner terminates its owned group before command.Wait reaps it.
func (unixProcessContainment) waitEmpty(context.Context) error { return nil }
