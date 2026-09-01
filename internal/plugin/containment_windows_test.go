//go:build windows

package plugin

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsContainmentConfiguresEnforcedJobLimits(t *testing.T) {
	contained, err := newProcessContainment()
	if err != nil {
		t.Fatal(err)
	}
	containment := contained.(*windowsProcessContainment)
	defer func() { _ = containment.close() }()

	var information windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(
		containment.job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	wantFlags := uint32(windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
		windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
		windows.JOB_OBJECT_LIMIT_JOB_MEMORY |
		windows.JOB_OBJECT_LIMIT_PROCESS_TIME |
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
	if information.BasicLimitInformation.LimitFlags&wantFlags != wantFlags {
		t.Fatalf("job flags = %#x, want %#x", information.BasicLimitInformation.LimitFlags, wantFlags)
	}
	if information.BasicLimitInformation.ActiveProcessLimit != 2 ||
		information.ProcessMemoryLimit != pluginProcessMemoryLimit ||
		information.JobMemoryLimit != pluginJobMemoryLimit ||
		information.BasicLimitInformation.PerProcessUserTimeLimit != pluginProcessTimeLimit {
		t.Fatalf("job limits = %#v", information)
	}
}
