---
name: repository-readiness-audit
description: Audit repository files, documentation audiences, installation examples, and release readiness when a prerelease, file-hygiene, or onboarding audit is requested. Not a substitute for feature testing or permission to publish.
---

# Routevane readiness audit

Apply these project criteria with `independent-audit` when available. Otherwise,
review directly against this file and the repository contracts. A clean clone
must not require a shared skill, user installation, or workstation path.

## Sources and boundaries

Read [AGENTS.md](../../../AGENTS.md) and
[current requirements](../../../docs/requirements.md). For executable checks use
[quality gates](../../rules/quality-gates.md); for release scope also read
[releasing](../../../docs/releasing.md). Reuse their commands and acceptance map.
Name the stage and audiences; a narrow documentation review is not a full release.

Audit is read-only toward implementation and live state. Use disposable data,
isolated tool/browser caches and only task-owned processes for permitted probes;
write evidence under ignored `tmp/`. No device changes, global installation,
history rewriting, publication, delegation or deletion of real data is implied.

## Project acceptance surfaces

- **Files and readers:** inventory root files, documentation/support directories
  and product source packages, excluding identified caches/generated dependencies
  and independent checkouts. Establish consumer, purpose, fact owner, freshness
  and disposition. Preserve unique requirements and unfinished work. Follow user,
  contributor, maintainer and extension-author entry paths; private history and
  internal plans must not replace current onboarding.
- **Guidance:** follow [README](../../../README.md),
  [installation](../../../docs/installation.md) and
  [contributing](../../../CONTRIBUTING.md) literally, including shipped plugin
  examples/manifests. Record prerequisites and rescue steps. Check vocabulary,
  platforms/versions, storage/update/deletion and security claims; investigate
  code-span paths, obsolete commit references and validator-only navigation,
  distinguishing intentional history. Do not substitute reconstructed examples.
- **Packages:** derive the required archive/platform matrix from current release
  guidance. Inspect every in-scope archive's contents, licenses/notices, metadata,
  paths and modes. From an extraction path with spaces and fresh data, exercise
  its real `start-routevane` launcher on each required native platform: version,
  health, supplied `catalog/`, UI and process cleanup. Confirm invalid inputs
  produce diagnostics and a failing result. Cross-builds and version-only runs
  do not establish installation.
- **Use and lifecycle:** follow README's Lists → Profiles → Build a profile →
  Connection/export journey on disposable state. Check start/stop, update,
  backup/restore, rollback, removal and troubleshooting where in scope. Protect
  `data/` and edited `catalog/`; they are not caches. Do not exercise real
  uninstall or device delivery merely to finish an audit.

## Acceptance evidence

Bind findings to commit/tree, relevant dirty/staged state and archive digest.
Report located expected/observed behavior, impact, coverage and missing checks;
separate fact, inference and uncertainty. Recommend corrections only when supported.

Use the requirements' open limits: rendering, validation, transport, activation,
physical devices, clean OS, CI and publication are distinct claims. External
comparisons require current primary sources. A known mandatory failure blocks
acceptance; missing mandatory evidence prevents a pass without proving a defect.

Put mechanical regressions in existing owner gates. Counts, phrases and document
length do not prove clarity. After separately authorized changes, recheck gates
and literal journeys against the final artifact.
