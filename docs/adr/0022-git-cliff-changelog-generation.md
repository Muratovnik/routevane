---
status: adopted
---

# ADR 0022: Generate the changelog with git-cliff and publish the curated entry

## Context

Two required artifacts do not exist. The adopted plan requires a `CHANGELOG.md`
that begins at the first published version, and nothing in this repository
produces one. `release.yml` publishes the same fixed sentence as the notes of
every release, so the notes describe the product rather than the version.

The cross-project release policy makes a generated-then-curated changelog the
shared format for this workstation's products, with git-cliff as the generator
and the Vue layout as the shape. SimStow adopted it first and validated both.

ADR 0002 forbids new tooling until a measured workflow gap requires it. The gap
is measured here: two artifacts a release cannot be cut without, and no tool in
the repository that writes either. The same ADR's pinning rule cannot be
followed literally — a Go tool directive pins Go programs and git-cliff is a
Rust binary — and `web/package-lock.json` locks the frontend, which is not the
home of a release-authoring tool.

## Decision

- Adopt git-cliff as the changelog generator. `cliff.toml` owns the layout:
  only `feat`, `fix`, `perf`, and `revert` commits appear, a breaking change
  always appears, and the releaser edits the rendered result before committing
  it.
- Invoke it on demand at a pinned version, `npx --yes git-cliff@2.13.1`. It is
  deliberately absent from `go.mod`, from `web/package.json`, and from every
  gate and CI job. Nothing automated consumes it: the changelog is a committed
  file, and the tool is how a human drafts that file a few times a year. The
  same binary ships on PyPI, which is how the first adopter pins it, but this
  repository pins no Python environment to hang that on and does pin npm.
- Release notes are the committed changelog entry, passed to `gh release
  create --notes-file`. They are never re-rendered from the commit log at
  publish time, because that would publish the uncurated text and silently
  discard the editing pass the policy exists to require.
- `release.yml` uses `relkit notes` to validate and extract the entry before the
  build and again before publication. The explicit `vue-like` profile in
  `relkit.toml` checks the final edited layout and commit links. Routevane keeps
  its annotated-tag/source-commit checks, but does not duplicate the notes parser.
- Write the comparison and commit links out against the module path in
  `go.mod`, which plugin authors already import, rather than resolving them
  through git-cliff's remote integration: that integration wants a token and a
  network call to add pull-request numbers this project has none of, because
  every commit reaches the default branch directly. The links resolve once the
  repository is published; a repository published under another name would
  have to change the module path first, and this file with it.
- GoReleaser stays deferred as ADR 0002 decided, and release-please and
  semantic-release are not adopted: both replace the manual curation step with
  automation, and one of them additionally requires a GitHub pull-request loop
  this project does not use.

## Consequences

The changelog is reviewed in the same commit as the version it describes, and a
release cannot be published for a version the changelog is silent about. The
notes an operator reads are the ones a person wrote.

git-cliff is fetched when it is used, so a releaser without network access
cannot render an entry. That is acceptable because the file, not the tool, is
the artifact: an entry can be written by hand in the same layout, and no gate
depends on the tool being present.

Rollback is a normal ADR supersession: delete `cliff.toml` and write entries by
hand. Nothing else in the repository refers to git-cliff.

Acceptance is `npx --yes git-cliff@2.13.1 --unreleased --tag vX.Y.Z` rendering
the entry for a release being cut, `tools/dev.ps1 check` staying green, and the
release workflow refusing a version the changelog does not describe or whose
edited entry violates the configured format. The maintained authoring and
validation procedure is in [the release guide](../releasing.md#changelog-contract).
