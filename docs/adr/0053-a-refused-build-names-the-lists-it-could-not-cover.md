---
status: adopted
---

# ADR 0053: a refused build names the lists it could not cover

This decision adds to the failed-attempt record of
[ADR 0004](0004-immutable-local-publication.md) and to the coverage rule of
[ADR 0010](0010-domain-first-and-no-network-expansion.md). The planner's
decision, the refusal itself and the immutability of attempts are unchanged.

## Context

A build is refused when the required part of a list has no rule the format can
carry. The refusal said only that coverage was incomplete, and the planner
stopped at the first uncovered component. The profile page could repeat the
sentence but could not say which list to open, so the operator searched a
composition of dozens of lists for the one with no usable source.

The page does not read the refusal from the response. It reads the last attempt
of each format, which is stored, so the names have to be stored with it or they
disappear on the next reload and for a build that ran from the scheduler.

## Decision

- **The planner names every uncovered list.** `BuildPlanSet` returns a
  `RequiredCoverageError` that still answers `errors.Is` to
  `ErrRequiredCoverage`. It carries identities only: the uncovered components
  and the sorted, distinct list identities among them. The planner knows nothing
  about titles, transport or storage.
- **The application adds titles.** `PartialCoverageError` unwraps to
  `ErrPartialCoverage`, so every existing check keeps answering. Its lists are
  `{id, title}`, sorted by identity, with the definition's title or the identity
  when the title is empty. They are bounded: at most 256 lists and 200 characters
  of title, which a real composition never reaches.
- **The response names them.** A build refused for coverage answers `422` as it
  did, with the same `code`, and adds `uncovered_lists: [{id, title}]`. No other
  refusal carries the field.
- **The attempt keeps them.** SQLite schema version 18 adds
  `output_attempts.uncovered_lists_json`, a JSON array defaulting to `[]`. The
  schema accepts a non-empty array only on a `partial_coverage` attempt, and the
  store checks the identities and the bounds before it writes. Attempts stay
  immutable. The title is the one the list had when the build ran; the link goes
  by identity, so a renamed list is still the list opened.
- **The page links them.** The failed-build notice lists the uncovered lists as
  links to the list in the library, `/lists#list=<id>`: the fragment is the
  library's own address and the service refuses a query string. An attempt
  recorded before this version, or a coverage failure that names no list, shows
  the sentence alone.

## Consequences

A version-18 database cannot be opened by an earlier binary; rollback restores a
backup taken before the upgrade. Attempts written earlier read back unchanged
with no lists.

The final preflight, which checks a plan again after composition, can still
refuse a plan on coverage when only the plan is at hand. It names the lists by
identity in that case, because it holds no titles; the planner refuses first in
every build the product runs.
