//go:build linux || darwin

package plugin

import (
	"errors"
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
	err := unix.Kill(-command.Process.Pid, unix.SIGKILL)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func (unixProcessContainment) close() error { return nil }
