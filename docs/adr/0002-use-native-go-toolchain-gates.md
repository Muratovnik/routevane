---
status: superseded
---

# ADR 0002: Use native Go toolchain gates with pinned analysis tools

> Superseded 2026-09-01 by
> [`0030-go-1-27-native-toolchain-gates.md`](0030-go-1-27-native-toolchain-gates.md).
> The native-gate design remains, but the supported toolchain and platform
> baseline changed.

## Context

Routevane needs reproducible Go checks on Windows and Linux before product code
exists. A large aggregate linter would add a second configuration vocabulary
and a release cadence that is not needed to validate this small module.

The current supported toolchain is Go 1.26.7. Staticcheck 0.7.0 supports the Go
1.26 language line, while `gosec` and `govulncheck` cover source-level security
findings and known dependency vulnerabilities. Go tool directives can pin these
executables in `go.mod` without a repository-specific installer.

## Decision

- Use `gofmt`, `go mod tidy -diff`, `go mod verify`, `go vet`, and `go test` as
  the base gate.
- Pin Staticcheck 0.7.0, gosec 2.28.0, and govulncheck 1.7.0 through Go tool
  directives and execute them with `go tool`.
- Require explanations on `#nosec` suppressions.
- Run the race detector on Linux CI, and run the ordinary gate on both Linux
  and Windows.
- Keep an empty nested module boundary at `web/go.mod` so root Go commands do
  not discover Go sources shipped inside npm dependencies.
- Do not add `goimports`, golangci-lint, a coverage percentage, GoReleaser, or a
  Make/Task wrapper until a measured workflow gap requires one.

## Consequences

The gate has fewer moving parts and each failure maps to an owning tool. It may
need a future Windows race job once the project contains concurrent code and a
supported C toolchain is guaranteed on contributor machines.

Rollback is a normal ADR supersession: remove the tool directives and replace
the individual commands only after the proposed aggregate gate is demonstrated
on both supported operating systems with equivalent security coverage.

Acceptance is `tools/dev.ps1 check-go` passing locally and in the two-OS CI
matrix, with `go.mod` and `go.sum` unchanged after the run.
