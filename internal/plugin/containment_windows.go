//go:build windows

package plugin

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	pluginProcessMemoryLimit = 512 << 20
	pluginJobMemoryLimit     = 768 << 20
	pluginProcessTimeLimit   = 300 * 10_000_000 // 100-nanosecond units
)

type windowsProcessContainment struct {
	job windows.Handle
}

func newProcessContainment() (processContainment, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create plugin job object: %w", err)
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
		windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
		windows.JOB_OBJECT_LIMIT_JOB_MEMORY |
		windows.JOB_OBJECT_LIMIT_PROCESS_TIME |
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	// The private runner waits while it is assigned, then starts exactly one
	// plugin child. A third process is rejected by the job.
	information.BasicLimitInformation.ActiveProcessLimit = 2
	information.BasicLimitInformation.PerProcessUserTimeLimit = pluginProcessTimeLimit
	information.ProcessMemoryLimit = pluginProcessMemoryLimit
	information.JobMemoryLimit = pluginJobMemoryLimit
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		// #nosec G103 -- the Windows API requires a pointer to its typed limit
		// structure and its exact size; no arbitrary address is constructed.
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure plugin job object: %w", err)
	}
	return &windowsProcessContainment{job: job}, nil
}

func (*windowsProcessContainment) prepare(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return nil
}

func (c *windowsProcessContainment) attach(command *exec.Cmd) error {
	if command.Process == nil {
		return fmt.Errorf("plugin runner did not start")
	}
	pid := command.Process.Pid
	if pid <= 0 || uint64(pid) > math.MaxUint32 {
		return fmt.Errorf("plugin runner returned invalid process id %d", pid)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open plugin runner process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(c.job, process); err != nil {
		return fmt.Errorf("assign plugin runner to job object: %w", err)
	}
	return nil
}

func (c *windowsProcessContainment) terminate(*exec.Cmd) error {
	if c.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(c.job, 1)
}

// Reaping the runner does not reap its Windows child. Job accounting is the
// native completion condition; the job handle itself is not signaled for an
// ordinary exit. Keep the job open until it is empty before removing the image.
func (c *windowsProcessContainment) waitEmpty(ctx context.Context) error {
	if c.job == 0 {
		return nil
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var accounting struct {
			TotalUserTime, TotalKernelTime, PeriodUserTime, PeriodKernelTime int64
			PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
		}
		if err := windows.QueryInformationJobObject(c.job, windows.JobObjectBasicAccountingInformation,
			// #nosec G103 -- Windows requires the address and exact size of its accounting structure.
			uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return fmt.Errorf("read plugin job completion: %w", err)
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for plugin descendants: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *windowsProcessContainment) close() error {
	if c.job == 0 {
		return nil
	}
	err := windows.CloseHandle(c.job)
	c.job = 0
	return err
}
