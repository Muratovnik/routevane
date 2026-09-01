---
status: adopted
---

# ADR 0030: Go 1.27 is the native toolchain baseline

Replaces
[`0002-use-native-go-toolchain-gates.md`](0002-use-native-go-toolchain-gates.md).

## Context

ADR 0002 fixed Go 1.26.7 as the supported toolchain while keeping Routevane's
quality gate on the native Go tools plus pinned analyzers. Go 1.27 is now the
maintained release used by the workstation, and the complete Routevane Go gate
passes with that toolchain. Keeping 1.26 as the documented line makes a normal
fresh checkout reject the installed maintained compiler and lets setup perform
dependency and hook work before reporting the incompatibility.

Go 1.27 also raises the supported macOS floor to macOS 13 Ventura. That is a
public compatibility change and must be stated rather than inferred from a
cross-compiled archive. The upstream release notes are authoritative:
<https://go.dev/doc/go1.27>.

## Decision

- Require Go 1.27.0 through the root module's language line. Do not add a
  redundant `toolchain go1.27.0` directive that Go's own tidy command removes.
  The empty nested web module uses the same language line.
- Keep the native gate from ADR 0002: `gofmt`, `go mod tidy -diff`, module
  verification, `go vet`, `go test`, the Linux race job, Staticcheck, gosec and
  govulncheck. The analyzers remain Go tool dependencies rather than global
  workstation installs.
- Run the repository doctor before setup downloads dependencies or installs
  contributor hooks. Operator startup never installs Git hooks; contributors
  opt in with `tools/dev.ps1 install-hooks`.
- Test the ordinary Go gate on Windows and Linux with Go 1.27. A Darwin release
  requires macOS 13 or newer and is not called natively accepted until its
  archive runs on that platform.
- Plugin examples and the repository template use the same minimum Go line as
  the public SDK they import.

## Consequences

Go 1.26 and older fail before setup changes checkout-local state. Users of a
downloaded release still need no Go installation. Supporting macOS 12 or older
would require a separately maintained older Go build line and is not promised.

Rollback is a replacing ADR and a coherent change to both module directives,
CI, doctor, launchers, documentation and plugin templates. Changing only one of
those surfaces is not a supported rollback.

Acceptance is a fresh source preflight with Go 1.27, `tools/dev.ps1 check-go`
on Windows and Linux, the browser gate with the Go-built product, and a release
archive reporting the stamped version. An unsupported Go line must fail before
dependency installation or hook mutation.
