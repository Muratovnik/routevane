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

Cutting a release (replace `v0.1.1` below with the new version; never reuse a
published tag):

1. For the repository's first release, enable **Release immutability** in
   GitHub's repository settings. The workflow refuses to accept a published
   release that GitHub does not report as immutable.
2. Gates green: `pwsh -File tools/dev.ps1 check` and
   `pwsh -File tools/dev.ps1 test-browser`.
3. Render the entry, then curate it by hand — a short Highlights paragraph for
   a minor, and prose for anything breaking. Paste the result into
   `CHANGELOG.md` below `## [Unreleased]` and above the previous release:

   ```powershell
   npx --yes git-cliff@2.13.1 --unreleased --tag v0.1.1
   ```

   Validate the edited entry, preserving the generated commit links:

   ```powershell
   New-Item -ItemType Directory -Force tmp | Out-Null
   python .github/relkit.pyz notes v0.1.1 --output tmp/release-notes.md
   ```

   A nonzero exit blocks the release. `audit` alone does not check release notes.

4. Commit as `chore(release): v0.1.1` after the staged audit. The history audit
   requires a clean worktree, including the curated changelog.
5. Run `python .github/relkit.pyz audit --history --owner` against that commit
   before the first public push and before creating the annotated tag. The tag
   is the publication trigger, so do not create or push it until the gates pass.
6. Push the tag. The release workflow binds the annotated stable SemVer tag to
   the event SHA, validates and exports the same curated entry before building
   and again before publication, reruns the canonical, race, browser, history
   and commit gates, builds and structurally verifies all five archives, and
   executes each archive on a native GitHub runner. It then signs provenance and SBOM attestations,
   verifies them, uploads a draft, compares every uploaded digest, and publishes
   the complete immutable release.
7. Download the archive and follow the verification below, then complete the
   [quick start](../README.md#download-and-start) with fresh data.

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
belongs to this release; the format check cannot establish either.

If a draft repeats an earlier release or omits the previous version's comparison
link, stop and check the checkout's tags and repository ownership. Render again
from a clean clone owned by the current user if necessary; do not simply change
the heading to make incorrect release boundaries pass validation.

When upgrading `.github/relkit.pyz`, adopt a reviewed deterministic artifact and
run the canonical gate, which exercises the shipped command with the actual
policy and invalid-note fixtures. After reviewing the artifact/configuration
change, refresh and verify this repository's owner guard with
`python .github/relkit.pyz protect install` and
`python .github/relkit.pyz protect check`, then run the clean history/owner audit.
Do not replace the user-scoped hook dispatcher from this repository.

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
