# Routevane repository instructions

Routevane is an independent Git repository for one product. These instructions
are complete for a task started at this root; do not rely on a parent workspace
to supply ownership, safety, verification, or commit rules.

Current product requirements and acceptance limits are in `docs/requirements.md`.
Explicit user requests and this file govern agent behavior. Record a real,
hard-to-reverse technical choice as an ADR.

## Start and tools

- Inspect Git status, current files, relevant task history, live registrations,
  and running processes before structural changes.
- Workstation services, planning bindings, memory, task routing, and personal
  data sources are user-scoped. This repository does not install or register
  them and must work from a clean clone without personal client configuration.
  Optional ignored local preferences are allowed and must not affect repository
  gates. Reviewed shared client settings may be published when portable and free
  of secrets, personal bindings, and mandatory workstation services.
- `.agents/` owns the project rules and canonical project skills. The tracked
  `.claude/skills/` files are discovery-only adapters that require the matching
  canonical skill to be read completely; `CLAUDE.md` imports this root contract.
- For Routevane Go milestones, use the repo skill `routevane-go-slice`.
- For any `web/` implementation, use the repo skill `routevane-ui-slice`.
- For requested prerelease, repository-hygiene, documentation, or installation
  audits, use the repo skill `repository-readiness-audit`.

## Temporary workspace ownership

- Keep task scratch files and disposable clones under ignored `tmp/<task>/`,
  manually created Git worktrees under `tmp/worktrees/<task>/`, and retained
  private evidence under `.private/`. Do not create sibling task directories
  beside Routevane, including for parallel implementation or audits.
- Resolve the owning checkout and destination, inspect link ancestors and verify
  containment and `git check-ignore` before creating temporary resources. Never
  track a nested worktree as a gitlink. If a tool cannot use a project-local
  location, keep execution local or report the limitation; do not silently
  choose an external checkout or change a shared client root.
- The creating task owns cleanup and must reconcile worktree registrations,
  processes and remaining files before closing. Preserve unique changes and
  retained evidence; move/remove registered worktrees with Git, then inspect
  disk residue. Never recursively clean all of `tmp/`, follow junctions during
  cleanup, or treat ignored content as automatically disposable. Report exact
  retained paths and reasons. Permanent private repositories and installed
  services are not temporary resources; relocation requires consumer checks.

## Product and architecture

- Deliver one working vertical outcome per milestone; do not build horizontal
  layers without a consumer.
- Do not create packages, interfaces, migrations, tables, registries, adapters,
  or manifests for a later milestone.
- Keep domain and planner code free of HTTP, DNS, SQLite, browser, renderer,
  deployer, and UI details. Interfaces are declared by their consumer.
- Observation validity and policy decisions are separate. Never store
  accepted/rejected/quarantined as global sighting state.
- Auto v1 is deterministic and reason-coded. One observed address never proves
  a WHOIS, RDAP, ASN, or broad CIDR rule.
- Published plans and artifacts are immutable. A failed candidate never removes
  the previous valid artifact.
- The Go core and CLI remain one binary, independently usable without Electron.
  The desktop package adds Electron and owns one child instance of that same
  binary (ADR 0037). Do not add worker/API binaries, external
  plugin hosting, PostgreSQL, Redis, Prometheus, or OpenTelemetry without the
  measured milestone requirement.

Read `.agents/rules/go-architecture.md` before changing Go product code,
`.agents/rules/web-architecture.md` before changing `web/src`,
`.agents/rules/security-boundaries.md` before any network/browser/device source,
and `.agents/rules/quality-gates.md` before changing tools, tests, CI, or gates.

## Verification

Run from the repository root:

```powershell
python .github/relkit.pyz audit
pwsh -NoLogo -NoProfile -File tools/dev.ps1 check
```

Before a tag or first public push, run the history and owner-policy gate:

```powershell
python .github/relkit.pyz audit --history --owner
```

For browser-facing work also run:

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 test-browser
```

Every milestone has an end-to-end test of its user-visible result. Unit tests do
not replace that test. Use Go fuzzing and properties for actual input boundaries
and invariants; use golden files only for renderer formats. There is no arbitrary
coverage percentage in the Core MVP.

A build is not live acceptance. If a task-owned process serves the rebuilt
artifact, restart only that process after confirming ownership and a rollback
path; otherwise launch an isolated task-owned process and clean it up after
acceptance. Then verify runtime identity and one real read-only path. Keep
generated databases, WAL files, artifacts, reports, caches, browser binaries,
logs, and secrets out of Git.
Write generated browser screenshots, traces, diffs and reports only below the
ignored `tmp/`; never place them beside tests or product assets.

## Git

After required gates pass, commit each coherent task-owned green increment by
default. Preserve unrelated work and pre-existing staged changes. Stage only
exact task-owned paths, run `python .github/relkit.pyz audit --staged`, inspect
the staged diff, and commit in one uninterrupted sequence; never leave a staged
handoff. Never push unless the user explicitly requests it.

Commit subjects follow Conventional Commits with the closed Angular type set:
`build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`,
`style`, and `test`; an optional lowercase scope; and `!` for a breaking change.
Do not bypass hooks or add machine authorship or vendor trailers.

## Documentation

Public documents under `docs/` declare `status: draft | adopted | superseded` in
frontmatter. Public documents do not carry owner planning-item ids;
`docs/README.md` files, when present, describe a directory and are exempt.
Historical explanation belongs in an ADR or the task summary; always-on
invariants belong here; conditional workflows belong in a skill; mechanical
facts belong in a validator.

Each maintained document has one audience and a distinct question it answers.
Public documentation serves users, contributors, maintainers, or extension authors.
Internal task briefs, execution plans, and audit reports belong in ignored
`.private/`, not anywhere in the published source tree. Retention, accuracy, or
`superseded` status alone does not justify publication. Public ADRs explain
technical decisions; they are not a store for task transcripts or work reports.
The current-tree gate rejects the known withdrawn working documents at their
former paths, not every document in a similarly named directory. Review audience
and purpose regardless of filename; a path check cannot establish either.
Public links must work without ignored local files. Keep the user entry point
free of implementation plans and internal work history.
Link to one owner for a fact rather than maintaining contradictory copies.
Historical status never establishes current support or acceptance.
Before deleting a document, account for unique requirements and unresolved work;
before removing a file, inspect its consumers, generated status and data ownership.
A prerelease verdict must cover file/document ownership and the documented clean
user/developer paths as well as code gates. Report untested environments explicitly.

Check each restriction against its protected outcome, audience, and lifecycle.
Distinguish local presence, a required dependency, inclusion in source, and release
publication. A prohibition at one boundary does not apply automatically to the
others. Test both a legitimate case that must pass and the violation that must fail;
do not turn historical cleanup names or prose keywords into general policy.

## Response style

Lead with the practical result or exact blocker. Distinguish confirmed facts,
reasoned conclusions, and unverified hypotheses. Report exact validation and
absolute paths while preserving unrelated dirty work.
