---
status: adopted
---

# ADR 0054: an output follows the revision of its own target's artifact fields

This decision narrows the format check that every output of
[ADR 0013](0013-list-unit-and-outputs.md) carries, and keeps the immutable
output identity of [ADR 0004](0004-immutable-local-publication.md). How a refused
format is shown and replaced is unchanged from
[ADR 0051](0051-profile-and-format-retirement-and-subscription-reissue.md).

## Context

Every output stores a `target_revision`, and a build refuses the output with
`ErrTargetChanged` when the stored value differs from the current one. The
profile page then asks the operator to add the format again. Adding it again
creates a new output and a new subscription link, so every router or client
that reads the old link has to be reconfigured.

The stored value was one hash of the JSON of every target definition in the
catalog. That JSON includes titles, English titles, the kind and the manual
installation hint and steps. Any text edit to any target, such as rewording an
installation step or adding a new target field, changed the value for every
output of every format. After an upgrade with such an edit, every existing
output failed to build. Releases in this cycle edited the target catalog twice
in this way.

## Decision

- **The revision belongs to one target.** `domain.TargetRevision` hashes the
  artifact fields of a single target definition. A build compares an output's
  stored revision with the revision of that output's own target, together with
  the format key, renderer and renderer version it already compared. The
  catalog no longer has a revision for all targets together, and the
  publication composition no longer takes one.
- **Artifact fields decide the artifact.** The revision covers the fields the
  planner, the renderers and publication read when they build a file:

  | Field | In the revision | Reason |
  | --- | --- | --- |
  | `FormatKey` | yes | names the format the renderer emits |
  | `RendererID` | yes | selects the renderer |
  | `RendererOptions` | yes | parameters a renderer would receive; empty and absent are the same |
  | every `Constraints` field | yes | the planner applies the rule kinds and the rule limit, publication applies the artifact size, and the list bounds must equal the split the renderer performs |
  | `ID` | no | the key the output already stores and looks up by |
  | `Title`, `TitleEN` | no | names on a screen |
  | `Kind` | no | groups routers apart from applications on a screen |
  | installation hint and steps, both languages | no | instructions for the operator |

  The revision encodes each artifact field under an explicit name and omits
  zero values. A constraint added later with a zero default therefore leaves
  the revision of every existing target unchanged, while a target that sets the
  new constraint gets a different revision.
- **A new field must be classified.** A test reflects over `TargetDefinition`
  and `TargetConstraints` and fails when a field is neither in the revision nor
  listed as presentation. A new field cannot silently enter or leave the
  revision.
- **Existing outputs are rebased once, in the running process.** SQLite schema
  version 19 cannot compute the new revision, because the catalog and the
  renderer registry are loaded by the process, not stored in the database. The
  migration therefore records every live output in `legacy_target_revisions` and
  closes that table to further inserts. It replaces `outputs_identity_immutable`
  with a trigger that keeps the identity immutable without the revision column,
  and adds `outputs_target_revision_immutable`. The new trigger refuses any
  change to the revision of an output that is not in the legacy table.
  `serve` calls `application.RebaseLegacyTargetRevisions` after it opens the
  store and before anything can build. For each marked output whose target is
  still served with the same format key, renderer and renderer version, the
  rebase writes the new revision and removes the marker. The revision is then
  immutable again. A marked output that was retired loses its marker without a
  change. The whole pass is one transaction.
- **The genuine case still refuses.** A marked output whose format key,
  renderer or renderer version no longer matches keeps its old revision and
  stays marked, so its build still fails with `ErrTargetChanged`. An output
  created after the upgrade is never marked, so a later change to its target's
  artifact fields refuses it, as before. The interface keeps its message for
  that case: the format description changed and the format has to be added
  again.

## Why the rebase runs in the process

Two other places could have done this work:

- A migration hook that receives computed revisions would have put catalog
  and renderer knowledge into the code that opens the store. Every command that
  opens the database, including the ones that never build, would then have to
  load the catalog and the renderers first.
- Rebasing every output on every start, without a marker, would accept any
  change to a target's artifact fields after one restart and would remove the
  refusal entirely.

The marker table limits the exception to outputs that existed before the
upgrade. The trigger states the same limit in the schema, so a writer that
bypasses the application gets the same answer. Only `serve` reaches the build
path that compares revisions. The command-line `build` does not use stored
outputs, and `deploy` reads published artifacts only.

## Consequences

- Rewording a target's title, kind or installation in a release no longer
  strands outputs or their subscription links.
- An upgraded output whose target is missing at the first start, for example
  because a plugin renderer is not installed, stays marked. A later start that
  serves that target with the same format and renderer rebases it.
- The rebase cannot tell which part of the old catalog-wide value changed,
  because that value hashed all targets together. An output whose target's
  constraints changed in the same upgrade is therefore rebased when its format
  key, renderer and renderer version still match. For built-in targets this
  cannot happen: `frozenFormats` pins their constraints in code. Only plugin and
  alias targets carry this one-time risk.
- Rollback is forward-only, as with every schema version. An older binary
  refuses a version 19 database. The marker table stays in the schema after
  every marker is gone; an empty table costs nothing.
