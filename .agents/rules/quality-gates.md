# Quality gates

Routed from `AGENTS.md`. Read this before changing tools, tests, CI, hook logic,
or a number a gate compares against.

`tools/dev.ps1 check` is the canonical local gate and CI composes the same
commands. Do not add a second runner whose result can drift.

- Go's own `gofmt`, module verification, `vet`, tests, race detector in CI, and
  fuzz support come first. `staticcheck` supplies the pinned general analyzers;
  `govulncheck` is the Go team's low-noise vulnerability scan.
- Tool modules are pinned in `go.mod` with Go's `tool` directive. Do not replace
  them with unversioned global installs.
- Frontend checks are typecheck, ESLint, Stylelint, Prettier, unit tests, and a
  production build. Browser and axe checks are blocking for browser-facing work
  and remain a separate local command because browser binaries are an owned
  dependency installed by `tools/dev.ps1 setup-browser`.
- Dead-code and duplication ratchets start only when the Nuxt route/auto-import
  graph and meaningful source volume exist. Adding a noisy zero-day threshold
  is not a quality improvement.
- A threshold only tightens. Never lower or disable a gate to land a change.
- `gosec` honors a `#nosec` annotation only with a written justification
  (`-nosec-require-justification`). An annotation is for a finding a peer
  protocol or platform forces on us, never for one our own code chose; the
  reason belongs in the annotation and, when it is load-bearing, in an ADR.
  Adding one is a reviewable exception, not a way around the gate.
- Run reports after the source has reached its final formatted state. A report
  against earlier bytes is not evidence for the committed tree.
- Validate example/template files through the same copy/build/install path the
  documentation tells a reader to use. Handwritten test substitutes do not
  establish that the shipped example works.
- Release smoke checks start the packaged launcher with fresh data and check
  identity, health, UI and cleanup; a version-only invocation is insufficient.
- The pre-commit hook clears Git's index/worktree environment before launching
  child tools so throwaway repositories and worktrees cannot inherit the wrong
  index.
