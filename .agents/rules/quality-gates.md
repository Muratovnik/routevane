# Quality gates

Routed from `AGENTS.md`. Read this before changing tools, tests, CI, hook logic,
or a number a gate compares against.

`tools/dev.ps1 check` is the canonical local gate and CI composes the same
commands. Do not add a second runner whose result can drift.

- Go's own `gofmt`, module verification, `vet`, tests, race detector in CI, and
  fuzz support come first. `staticcheck` supplies the pinned general analyzers;
  `nilness` checks nil paths and `govulncheck` scans known Go vulnerabilities.
- Tool modules are pinned in `go.mod` with Go's `tool` directive. Do not replace
  them with unversioned global installs.
- Web and desktop checks run `npm audit` against their lockfiles, including
  development, optional and peer dependencies: Electron ships with the product
  even though npm classifies it as a development dependency. High and critical
  findings block the gate; lower severities remain visible for triage. Registry
  errors fail the check. Review dependency paths and compatible fixes; never use
  an automatic forced major upgrade to make the gate green.
- Frontend checks are typecheck, ESLint, Stylelint, Prettier, unit tests, and a
  production build. Unit component specs render in the pinned Chromium through
  Vitest Browser Mode, so `check` requires the browser `tools/dev.ps1
  setup-browser` installs and asserts it is there before npm runs. The Playwright
  end-to-end and axe suites stay a separate command for their runtime and because
  they drive the built product, not because of the binary; they remain blocking
  for browser-facing work.
- ESLint runs without `--max-warnings`: an error is the gate and a warning is
  advice. The warn-level rules are the advisory size and complexity signals
  (`max-lines`, `max-lines-per-function`, `complexity`,
  `vue/max-lines-per-block`, `sonarjs/cognitive-complexity`); everything a gate
  must catch is error level, and
  a preset's own `warn` is promoted so an upgrade cannot quietly downgrade a
  gate. Read an advisory warning as a place to look, not as a limit: do not
  split a component, a function or a test for its own sake to silence one.
- Dead-code and duplication ratchets start only when the Nuxt route/auto-import
  graph and meaningful source volume exist. Adding a noisy zero-day threshold
  is not a quality improvement.
- Never lower quality/security thresholds or bypass a justified gate to land a
  change. Correct false positives against the owning requirement, with regression
  tests for both the legitimate case and the protected failure. Narrowing an
  overbroad check is not permission to remove its actual safety boundary.
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
