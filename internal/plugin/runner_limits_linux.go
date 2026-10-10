//go:build linux

package plugin

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func holdPluginRunnerThread() func() {
	// Pdeathsig is tied to the creating thread. Keep that thread alive until
	// the child has exited or the runner terminates its owned process group.
	runtime.LockOSThread()
	return runtime.UnlockOSThread
}

func preparePluginRunnerLimits() error { return nil }

func startLimitedPlugin(command *exec.Cmd, termination <-chan os.Signal) (err error) {
	// exec stops before the loader or plugin runs. The supervisor keeps its own
	// address space available, including the race detector's shadow mappings.
	command.SysProcAttr = &syscall.SysProcAttr{Ptrace: true, Pdeathsig: syscall.SIGKILL}
	if err := command.Start(); err != nil {
		return err
	}
	resumed, reaped := false, false
	defer func() {
		if !resumed {
			if !reaped {
				if killErr := command.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) && !errors.Is(killErr, unix.ESRCH) {
					err = errors.Join(err, fmt.Errorf("kill stopped plugin: %w", killErr))
				}
			}
			if waitErr := command.Wait(); waitErr != nil {
				var exit *exec.ExitError
				if !errors.As(waitErr, &exit) && !(reaped && errors.Is(waitErr, unix.ECHILD)) {
					err = errors.Join(err, fmt.Errorf("reap stopped plugin: %w", waitErr))
				}
			}
		}
	}()
	if reaped, err = waitPluginExecStop(command.Process.Pid, termination); err != nil {
		return err
	}
	if err := checkPluginMappedAddressSpace(command.Process.Pid); err != nil {
		return err
	}
	for _, limit := range pluginResourceLimits() {
		value := unix.Rlimit{Cur: limit.value, Max: limit.value}
		if err := unix.Prlimit(command.Process.Pid, limit.resource, &value, nil); err != nil {
			return fmt.Errorf("set child resource limit %d: %w", limit.resource, err)
		}
		var observed unix.Rlimit
		if err := unix.Prlimit(command.Process.Pid, limit.resource, nil, &observed); err != nil {
			return fmt.Errorf("read child resource limit %d: %w", limit.resource, err)
		}
		if observed != value {
			return fmt.Errorf("child resource limit %d differs from its required bounds", limit.resource)
		}
	}
	select {
	case <-termination:
		return errors.New("plugin startup terminated")
	default:
	}
	if err := unix.PtraceDetach(command.Process.Pid); err != nil {
		return fmt.Errorf("resume limited plugin: %w", err)
	}
	resumed = true
	return nil
}

func waitPluginExecStop(pid int, termination <-chan os.Signal) (reaped bool, err error) {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var status unix.WaitStatus
		observed, err := unix.Wait4(pid, &status, unix.WNOHANG, nil)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return false, fmt.Errorf("wait for plugin exec stop: %w", err)
		}
		if observed == pid {
			if status.Stopped() && status.StopSignal() == unix.SIGTRAP {
				return false, nil
			}
			return status.Exited() || status.Signaled(), errors.New("plugin did not enter its exec stop")
		}
		select {
		case <-termination:
			return false, errors.New("plugin startup terminated")
		case <-deadline.C:
			return false, errors.New("plugin exec stop timed out")
		case <-tick.C:
		}
	}
}

func checkPluginMappedAddressSpace(pid int) error {
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return fmt.Errorf("read stopped plugin address space: %w", err)
	}
	defer func() { _ = file.Close() }()
	const maxStatmBytes = 4096
	value, err := io.ReadAll(io.LimitReader(file, maxStatmBytes+1))
	if err != nil || len(value) > maxStatmBytes {
		return errors.New("unable to read bounded stopped plugin address space")
	}
	fields := strings.Fields(string(value))
	if len(fields) != 7 {
		return errors.New("invalid stopped plugin address space")
	}
	pages, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return fmt.Errorf("parse stopped plugin address space: %w", err)
	}
	// exec has already mapped the image. Lowering RLIMIT_AS does not remove
	// existing mappings, so refuse an oversized image before any code resumes.
	pageSize := os.Getpagesize()
	if pageSize <= 0 {
		return errors.New("invalid system page size")
	}
	if pages > pluginAddressSpaceLimit/uint64(pageSize) {
		return errors.New("stopped plugin image exceeds its address space limit")
	}
	return nil
}
