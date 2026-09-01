---
name: repository-readiness-audit
description: Audit repository files, documentation audiences, installation examples, and release readiness when a prerelease, file-hygiene, or onboarding audit is requested. Not a substitute for feature testing or permission to publish.
---

# Repository readiness audit

Use the repository contract and the user's requested scope. Audit requests are
read-only unless cleanup or implementation is also authorized. Do not infer
permission for history rewriting, deleting user data, installing global services,
or publishing. Delegation requires separate authorization.

## Evidence to collect

- Freeze commit/tree and dirty/staged paths. Separate tracked source, generated
  output, private/runtime data, vendored tools, and the actual distribution.
- Inventory every root file and every documentation/support directory. Inspect
  content and callers, not just names. Record reader or executable consumer,
  unique purpose, authoritative fact owner, freshness, and disposition:
  keep, merge, relocate, retire, or unresolved.
- Trace user, contributor, maintainer and extension-author entry paths from their
  first visible file. A user should not need internal plans to install a release.
  Count documents to understand scope, never as a target to minimize.
- Compare overlapping facts: vocabulary, supported versions/platforms, commands,
  storage locations, deletion/update behavior, security guarantees, and support
  claims. Mark drafts/history and explicit replacements. Broken code-span paths,
  obsolete commit references, and documents reachable only from validators
  deserve investigation just as broken Markdown links do.
- Before retiring material, identify unique requirements and unfinished work.
  Preserve real decisions/history where useful; do not move duplicate prose
  into a new folder and call the conflict resolved.

## Exercise what the reader receives

- Follow documented commands literally with fresh data and isolated tool/browser
  caches. A clean checkout on a prepared workstation is not a clean environment.
  Record inherited prerequisites and any manual rescue steps.
- Include a checkout/extraction path with spaces. Confirm a deliberately invalid
  input still fails its gate and that the command exposes the actual diagnostic.
- Verify that tool traversal covers the product's source packages without treating
  ignored caches, nested checkouts or generated dependencies as product code.
- Build examples using their shipped templates/manifests. Tests that reconstruct
  equivalent files do not prove the instructions or supplied files work.
- Inspect every archive's inventory, licenses, notices, metadata, paths and modes.
  Start its real launcher on each claimed native platform; check version, health,
  UI/catalog and process cleanup. Version-only execution is not installation.
- Check start, stop, first safe use, update, backup/restore, uninstall, and
  troubleshooting. Never exercise destructive device actions without authority.
- Compare public claims with exact evidence: rendering, validation, transport,
  consumer activation, physical-device behavior, clean OS, CI and publication
  are separate claims. External comparisons need current primary sources;
  unsourced positioning is not a requirement or confirmed advantage.

## Verdict and prevention

Report prioritized findings with a concrete file/line, contradiction or failed
reader action, impact, and smallest sound correction. Distinguish observed facts
from inference and untested scope. Include dispositions, protected data, exact
oracles and source identity; do not declare ship from code gates alone.

Automate stable mechanical contracts such as local links, version alignment,
example-file consumption, archive inventory and native launcher smoke. Keep
audience clarity and document meaning as explicit human-review criteria. Do not
replace semantic review with brittle phrase, length or document-count checks.
After authorized changes, rerun affected gates and repeat the literal journeys
against the final artifact, then report residual limitations.
