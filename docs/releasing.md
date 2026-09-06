---
status: adopted
---

# Releasing Routevane

A release is an annotated `vX.Y.Z` tag, and pushing that tag is what builds and
publishes the archives. Versions follow
[Semantic Versioning](https://semver.org/); while Routevane is `0.y.z` a
breaking change raises the minor and everything else raises the patch.

A version protects four surfaces: the local HTTP API, the catalog format, the
CLI flags, and the layout of the release archive. Package boundaries, the
database schema, and the generated UI internals are not part of it and may
change in any release.

The tagged source is the release identity. `tools/dev.ps1 release` derives it from
`git describe` and stamps it into the binary, so the tag is the single source
and `routing-agent version` reports it back.

## Managed release

The vendored release-kit provides `release plan`, `release run` and
`release resume`. This integration is experimental until a separately approved
new tag completes the whole publication flow. Existing releases without a
coordinator receipt cannot be adopted as unfinished runs or republished.

Prerequisites are a normal full-history checkout (not a linked worktree), GitHub
CLI 2.98.0 or newer, locked developer/browser dependencies, current project hooks
and the maintainer's private owner policy. The active tag workflow must publish
immutable releases with provenance for all five archives, the Windows desktop
installer, its blockmap, `latest.yml` and `SHA256SUMS`.
The host must have a shipped native archive (macOS requires ARM64).

Cutting a release (replace `vX.Y.Z` below with an agreed new version; never reuse a
published tag):

1. For the repository's first release, enable **Release immutability** in
   GitHub's repository settings. The workflow refuses to accept a published
   release that GitHub does not report as immutable.
2. Agree the version and intended changes. `relkit.toml` binds preparation to the
   first numbered changelog entry; the build continues to stamp the Git tag.
3. Render the entry, then curate it by hand — a short Highlights paragraph for
   a minor, and prose for anything breaking. Paste the result into
   `CHANGELOG.md` below `## [Unreleased]` and above the previous release:

   ```powershell
   npx --yes git-cliff@2.13.1 --unreleased --tag vX.Y.Z
   ```

   Validate the edited entry, preserving the generated commit links:

   ```powershell
   New-Item -ItemType Directory -Force tmp | Out-Null
   python .github/relkit.pyz notes vX.Y.Z --output tmp/release-notes.md
   ```

   A nonzero exit blocks the release. `audit` alone does not check release notes.

4. Commit as `chore(release): vX.Y.Z` after the staged audit. The history audit
   requires a clean worktree, including the curated changelog.
5. Review `python .github/relkit.pyz release plan vX.Y.Z`: exact commit, previous
   tag, notes, assets, workflow/jobs and plan SHA-256. Only the new tag is configured
   for push; no branch or other refs are pushed. The tag still exposes every commit
   reachable from it. Local and remote tags must agree: reconcile drift explicitly,
   never rewrite a tag or guess the predecessor from generator output.
6. After explicit authorization of that exact publication, run:

   ```powershell
   python .github/relkit.pyz release run vX.Y.Z --publish --plan-hash REVIEWED_SHA256
   ```

   The command runs `tools/dev.ps1 check`, `tools/dev.ps1 test-browser`, worktree
   and history/owner audits and the guard check before creating an annotated tag
   and pushing its exact ref. Existing Git hooks still run. No automatic commit,
   stash, force-push, hook bypass or repository-setting change is performed.

   The release workflow binds the annotated stable SemVer tag to
   the event SHA, validates and exports the same curated entry before building
   and again before publication, reruns the canonical, race, browser, history
   and commit gates, builds and structurally verifies all five archives, and
   executes each archive on a native GitHub runner. It then signs provenance and SBOM attestations,
   verifies them, uploads a draft, compares every uploaded digest, and publishes
   the complete immutable release.
7. The coordinator observes the tag's exact successful workflow run and configured
   jobs, then downloads the exact nine assets. It checks notes, sizes, digests, the
   checksum manifest, immutable-release signatures and provenance bound to the
   same repository, source commit, workflow and CI attempt. It selects this host's
   archive and runs `tools/release_smoke.py` from pinned source: real launcher,
   version, health, embedded UI, catalog, fresh data and process cleanup. CI covers
   the other native platforms.

After an interruption, inspect the diagnostics and continue with
`python .github/relkit.pyz release resume vX.Y.Z --publish`. Use the same checkout
and release-kit version. Resume reconciles the server first; it does not recreate
releases, repair drafts or rerun CI. A manually reviewed newer CI attempt requires
`--accept-ci-attempt N`. New local edits after the push remain separate and untouched.
Publication may already have succeeded when verification fails; never delete or
overwrite that release as automatic recovery.

Receipts/logs stay in `.git/relkit/releases/vX.Y.Z/`, scratch in `.git/relkit/tmp/`.
Success cleans owned downloads and snapshots; retained unowned files are reported.
Standalone archive smoke uses ignored `tmp/`. No diagnostic or backup may be placed
outside the project without explicit agreement on its exact path. A local rehearsal
is not live publication acceptance and grants no permission to push.

## Changelog contract

`cliff.toml` prepares a draft; `[changelog]` in `relkit.toml` enables release-kit's
`vue-like` validation of the final edited entry. Only the requested version is
checked. Empty `Unreleased` sections and older entries do not block adoption.

Use a version/date heading with a comparison link. The explicitly declared first
version, `0.1.0`, may instead link to its release. Ordinary sections (`Features`,
`Bug Fixes`, `Performance Improvements`, `Reverts`) contain top-level bullets,
each with at least one inline commit link whose short hash matches its target.
Related changes may share a bullet; scope and PR links are optional. Preserve
links when consolidating the generated draft. `Highlights` and `BREAKING CHANGES`
may contain explanatory prose and migration examples without commit links.

`relkit notes` rejects missing, empty, duplicate or malformed entries before
writing anything. Use `--output`, not shell redirection, to preserve an existing
notes file when validation fails. The exported text is not regenerated or
reworded. Review still owns factual completeness and whether each linked commit
belongs to this release; the standalone format check cannot establish either.
The coordinator additionally validates Git boundaries and supplied ordinary change
links, while editorial completeness remains a review responsibility.

If a draft repeats an earlier release or omits the previous version's comparison
link, stop and check the checkout's tags and repository ownership. Render again
from a clean clone owned by the current user if necessary; do not simply change
the heading to make incorrect release boundaries pass validation.

When upgrading `.github/relkit.pyz`, first use `python .github/relkit.pyz update
--dry-run`, review the artifact and owned-guard digests, then repeat with `--yes`
only after approving those changes. A reviewed local development artifact uses
`--artifact PATH --sha256 EXPECTED_SHA256` in both commands. The updater retains
its rollback backup under `.git/relkit-update-*/` and leaves the global dispatcher
unchanged. Run the canonical gate, which exercises the shipped command with the
real policy and invalid-note fixtures. Separately reviewed policy/manual artifact
changes require `update --refresh-guard --dry-run`, then `update --refresh-guard
--yes` and `protect check`. The upgrade does not enable new config or publish.

## Verify a download

Verify the checksum using the [installation guide](installation.md#verify-a-download), then
verify the signed source/workflow identity:

   ```powershell
   gh attestation verify .\routevane-v0.1.0-windows-amd64.zip --repo Muratovnik/routevane --signer-workflow Muratovnik/routevane/.github/workflows/release.yml
   ```

Confirm `routing-agent version` reports the selected tag. A successful version
command alone does not establish that the launcher, catalog, UI, or persistence works.

Create/configure a remote only with explicit publication authorization. Never infer
permission to push from a successful local build.


## Local acceptance before publication

Run `tools/dev.ps1 release -Version v0.1.0` to build the archive set from the
current source. This is not a bit-for-bit reproducibility promise. Checksums and
attestations describe the actual published bytes.

The archive verifier checks inventory, licensing, SBOM, checksums and Unix modes.
`tools/release_smoke.py` extracts into fresh task-owned state, starts the shipped
launcher, checks version/health/embedded UI/catalog, and terminates its process
tree. Native CI runs this on every published platform. It does not contact devices.

A prerelease additionally needs the repository-readiness audit: file inventory,
audience paths, current-vs-historical documentation, real example files, isolated
browser/tool caches, data preservation, and external acceptance limits. A green
linter is not a verdict on those properties. Record the exact commit/tree reviewed.

Keep physical-device acceptance, clean OS checks, hosted CI, and publication
separate from local test results. Enabling immutable releases and configuring
repository permissions are external prerequisites, not actions a local gate proves.

### Windows desktop installer

Build the installer with `tools/dev.ps1 installer -Version vX.Y.Z` (substitute
the intended release version). The executable, its blockmap and `latest.yml` are
staged with `DESKTOP-SHA256SUMS` under `.cache/desktop-release/`. Building never
publishes. The release workflow uploads the three files to the same GitHub
release as the CLI archives and lists them in that release's single
`SHA256SUMS`; `DESKTOP-SHA256SUMS` only carries the bundle between jobs and is
not published. Do not upload just `latest.yml` or reuse metadata from a
different installer.

`tools/dev.ps1 test-update` builds three isolated NSIS fixture versions and
exercises manual upgrade, discovery, a rejected corrupt download, retry,
automatic restart, and preserved data. It uses a loopback feed, a unique
installer identity and a temporary profile; production builds always use
`Muratovnik/routevane`. Development and unpacked directory builds do not offer
application updates. The installer is currently unsigned; signing is separate
work recorded in [requirements](requirements.md#open-work-and-limits).
