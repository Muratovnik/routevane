# Contributing to Routevane

## Before changing code

Read `AGENTS.md` and the adopted implementation plan. Work one vertical
milestone at a time. A new abstraction needs a real consumer; a later milestone
does not justify an empty package, table, interface, or placeholder today.

## Setup and gates

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 setup
pwsh -NoLogo -NoProfile -File tools/dev.ps1 check
```

The full gate checks repository metadata, skills and docs, Go formatting,
module integrity, lint, tests, build, vulnerability data, and the frontend
type/lint/style/format/unit/build gate. Browser accessibility tests are a
separate command because they own browser binaries:

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 test-browser
```

Do not claim a live UI result after rebuilding while an older listener is still
serving. Restart the process that owns the artifact, then verify the visible
runtime.

## Commit messages

Use `type(optional-scope): imperative summary`, lower case, no trailing period,
and at most 72 characters. Allowed types are:

```text
build chore ci docs feat fix perf refactor revert style test
```

Use `!` for a breaking change and explain migration in a `BREAKING CHANGE:`
footer. Stage exact task-owned paths, inspect the staged diff, and do not bypass
the repository or user-scoped hooks.

## Security

Network-derived data is untrusted. Keep source validation, bounded execution,
redirect policy, DNS rebinding protection, and secret-safe logging in the same
vertical slice as the feature that needs them. See `SECURITY.md`.
