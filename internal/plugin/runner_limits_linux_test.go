//go:build linux

package plugin

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func buildResourceLimitProbe(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "probe")
	command := exec.Command("go", "build", "-o", executable, "./internal/plugin/testdata/resourcelimits")
	command.Dir = repositoryRoot(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build resource limit probe: %v\n%s", err, output)
	}
	return executable
}

func TestLinuxPluginChildHasExactLimitsBeforeItRuns(t *testing.T) {
	var inheritedAddressSpace unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_AS, &inheritedAddressSpace); err != nil {
		t.Fatal(err)
	}
	executable := buildResourceLimitProbe(t)
	runner, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, runner, processRunnerCommand, executable, directory)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Stdin = bytes.NewReader([]byte{processRunnerGate})
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() ||
		exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("runner must terminate its owned group: %v\n%s", err, diagnostics.String())
	}
	var report struct {
		Limits                 map[string]unix.Rlimit
		AllocationRefused      bool
		OpenRefused            bool
		PID                    int
		Group                  int
		SupervisorAddressSpace unix.Rlimit
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("plugin never reported its limits: %v\n%s\n%s", err, output, diagnostics.String())
	}
	if report.SupervisorAddressSpace != inheritedAddressSpace {
		t.Errorf("supervisor address space = %#v, want inherited %#v", report.SupervisorAddressSpace, inheritedAddressSpace)
	}
	if len(report.Limits) != 4 {
		t.Fatalf("plugin did not report all four limits: %#v", report.Limits)
	}
	for name, value := range map[string]uint64{"address_space": 2147483648, "cpu": 300, "files": 128, "core": 0} {
		if observed := report.Limits[name]; observed.Cur != value || observed.Max != value {
			t.Errorf("%s limit = %#v, want soft and hard %d", name, observed, value)
		}
	}
	if !report.AllocationRefused || !report.OpenRefused {
		t.Fatalf("plugin exceeded resource bounds: %#v", report)
	}
	if report.Group != command.Process.Pid || report.PID == report.Group {
		t.Fatalf("plugin did not inherit the trusted runner's group: %#v, runner=%d", report, command.Process.Pid)
	}
}

const resourceFailureProbe = "ROUTEVANE_TEST_RESOURCE_FAILURE"

func TestLinuxPluginLimitFailureDoesNotStartPluginCode(t *testing.T) {
	executable := buildResourceLimitProbe(t)
	runner, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for mode, reason := range map[string]string{"limit": "resource limit", "ptrace": "operation not permitted"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runner, "-test.run=^TestLinuxPluginLimitFailureProbe$", "--", executable, directory)
			command.Env = append(os.Environ(), resourceFailureProbe+"="+mode)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			output, err := command.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 126 || !strings.Contains(string(output), reason) {
				t.Fatalf("%s failure was not refused: %v\n%s", mode, err, output)
			}
			if _, err := os.Stat(filepath.Join(directory, "started")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("plugin code ran despite failed containment: %v", err)
			}
		})
	}
}

func TestLinuxPluginLimitFailureProbe(t *testing.T) {
	mode := os.Getenv(resourceFailureProbe)
	if mode == "" {
		return
	}
	// This disposable supervisor cannot raise its child's inherited hard limit.
	// No resource policy on the test process or workstation is changed.
	switch mode {
	case "limit":
		if err := unix.Setrlimit(unix.RLIMIT_CPU, &unix.Rlimit{Cur: 100, Max: 100}); err != nil {
			t.Fatal(err)
		}
	case "ptrace":
		runtime.LockOSThread()
		// Deny only ptrace on this disposable thread and its forked child.
		// The parent test and all workstation processes retain their policy.
		filter := []unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_PTRACE, Jf: 1},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		}
		program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
			t.Fatal(err)
		}
		runtime.KeepAlive(filter)
	default:
		t.Fatal("unknown failure probe mode")
	}
	if len(os.Args) < 4 {
		t.Fatal("missing limit probe arguments")
	}
	os.Exit(runContainedExecutable(os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]))
}

func TestLinuxPluginOversizedExecImageDoesNotStartPluginCode(t *testing.T) {
	executable := buildResourceLimitProbe(t)
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	image, err := elf.NewFile(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = image.Close() }()
	lastLoad := -1
	for index, program := range image.Progs {
		if program.Type == elf.PT_LOAD {
			lastLoad = index
		}
	}
	if lastLoad < 0 {
		t.Fatal("probe has no loadable segment")
	}
	// Increase the final segment's zero-filled memory without enlarging the
	// file. The kernel maps it at exec; plugin code must never reach its marker.
	var offset uint64
	var program bytes.Buffer
	switch image.Class {
	case elf.ELFCLASS64:
		var header elf.Header64
		if err := binary.Read(bytes.NewReader(payload), image.ByteOrder, &header); err != nil {
			t.Fatal(err)
		}
		offset = header.Phoff + uint64(lastLoad)*uint64(header.Phentsize)
		var segment elf.Prog64
		if err := binary.Read(bytes.NewReader(payload[offset:]), image.ByteOrder, &segment); err != nil {
			t.Fatal(err)
		}
		segment.Memsz += 3 << 30
		if err := binary.Write(&program, image.ByteOrder, segment); err != nil {
			t.Fatal(err)
		}
	case elf.ELFCLASS32:
		var header elf.Header32
		if err := binary.Read(bytes.NewReader(payload), image.ByteOrder, &header); err != nil {
			t.Fatal(err)
		}
		offset = uint64(header.Phoff) + uint64(lastLoad)*uint64(header.Phentsize)
		var segment elf.Prog32
		if err := binary.Read(bytes.NewReader(payload[offset:]), image.ByteOrder, &segment); err != nil {
			t.Fatal(err)
		}
		segment.Memsz += 3 << 30
		if err := binary.Write(&program, image.ByteOrder, segment); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown ELF class")
	}
	copy(payload[offset:], program.Bytes())
	if err := os.WriteFile(executable, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	runner, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, runner, processRunnerCommand, executable, directory)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Stdin = bytes.NewReader([]byte{processRunnerGate})
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 126 || !strings.Contains(string(output), "image exceeds its address space limit") {
		t.Fatalf("oversized image was not refused before userspace: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(directory, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized image executed plugin code: %v", err)
	}
}
