---
status: adopted
---

# ADR 0046: apply Linux plugin limits at the exec stop

## Context

The private plugin runner remains the trusted process-group leader while an
installed plugin runs (ADR 0009). Applying the plugin's 2 GiB address-space limit
to that supervisor also restricts its own runtime. ThreadSanitizer reserves a
large virtual address space, so a race-instrumented supervisor can fail to map
its bookkeeping after receiving this limit.

Go's process-launch API has no general child-resource-limit field or pre-exec
callback. Setting limits on an already running plugin would permit its startup
code to execute before containment. A separate helper binary, a shell wrapper,
or copied Go fork internals would add another runtime or maintenance boundary.

## Decision

On Linux, use Go's `SysProcAttr.Ptrace` and the existing `golang.org/x/sys/unix`
APIs. The kernel stops the child after loading its executable image and before
its dynamic loader or other userspace code executes. The supervisor keeps its
own resource limits and remains the process-group leader.

Before resuming that exact stopped child:

- Refuse an image whose mapped address space already exceeds 2 GiB. Lowering
  `RLIMIT_AS` alone does not remove existing mappings.
- Set and read back both soft and hard bounds: address space 2 GiB, CPU time
  300 seconds, 128 open files, and no core dumps.
- Refuse an unexpected stop, cancellation, inaccessible process metadata,
  failed limit operation, or failed detach. Kill and reap the unreleased child;
  do not retry without containment.

The child inherits the supervisor's process group. `Pdeathsig=SIGKILL` protects
the interval before the exec stop as well as the running child. Because Linux
ties that signal to the creating thread, the supervisor keeps that OS thread
locked throughout the child's lifetime. Existing group termination handles
normal plugin exit and host cancellation without signalling a recycled group.

The Windows Job Object and macOS inherited-limit paths retain their existing
behavior. This replaces only ADR 0009's Linux limit-installation mechanism;
the protocol, checksums, permissions and publication checks retain their owners.

## Consequences

The Linux host needs a readable `/proc`, `prlimit`, and permission to trace its
own child. A seccomp or tracing policy that denies these operations causes a
plugin startup failure. Routevane does not change host policy or silently run an
unlimited plugin. The CLI without external plugins does not need this path.

The kernel has already mapped the executable before the limits are installed.
The mapped-image check prevents an oversized image from executing; this is not
a bound on every kernel operation involved in loading an image. Resource
containment remains distinct from a malicious-code filesystem or network
sandbox.

The existing Go runtime and Unix APIs own process creation, tracing and limits;
Routevane owns their startup sequence and refusal behavior. Linux tests exercise
the real stopped-child boundary, exact bounds, resource refusal and startup
failure controls. The existing plugin end-to-end and descendant-lifetime tests
remain required, including the full Linux race gate.

References: [Go process launch](https://go.dev/src/syscall/exec_linux.go),
[Linux ptrace](https://man7.org/linux/man-pages/man2/ptrace.2.html),
[Linux resource limits](https://man7.org/linux/man-pages/man2/getrlimit.2.html),
and [ThreadSanitizer virtual memory](https://releases.llvm.org/20.1.0/tools/clang/docs/ThreadSanitizer.html#limitations).
