---
status: adopted
---

# ADR 0034: Keenetic route descriptions come from plan provenance

This extends ADR 0032's exact static-route ownership. It does not change the
Keenetic BAT renderer, its profile identity, or its published bytes.

## Context

Keenetic shows a Description for a static route. Its user documentation also
documents comments on imported BAT routes and says KeeneticOS 5.0 preserves
those comments during import and export. It does not specify the RCI JSON
property used by the web interface or a maximum description length.

Maintained Keenetic client implementations independently send a `comment`
property when creating a static route. That agreement is useful protocol
evidence, but it is not a vendor compatibility promise. Routevane must therefore
detect an omitted or transformed comment through read-back rather than treating
an HTTP success response as acceptance.

The published BAT file is a user-facing immutable artifact. Adding a BAT comment
would change existing renderer bytes and would conflate the officially
documented batch-note syntax with an undocumented RCI field.

Sources:

- [Keenetic static routing documentation](https://support.keenetic.com/titan/kn-1811/en/15880.html)
- [KeeneticOS 5.0 release notes](https://support.keenetic.com/buddy-6-se/kn-4410/en/100115-os-5-0.html)
- [`KeeneticPy` static-route client](https://github.com/keyiflerolsun/KeeneticPy)
- [`keenetic-ruby` static-route client](https://github.com/antonzaytsev/keenetic-ruby)
- [`keenetic-routes` RCI client](https://github.com/vladpi/keenetic-routes)

## Decision

- Every planned rule carries canonical, sorted, unique human labels. A label is
  `(category title/list title)`, using the effective category overlay and the
  resolved list definition title. A list in several current categories carries
  every real label; a list in none uses `Без категории`.
- Title characters that collide with the parentheses, slash, or multi-owner
  suffix grammar are replaced by their full-width forms. Controls become
  whitespace. Unicode text otherwise remains intact. Labels enter the immutable
  plan JSON and semantic hash; old snapshots without labels remain valid.
- Deployment joins validated BAT prefixes with the snapshot's labels. A prefix
  contributed by several planned rules receives the union of their labels even
  though the BAT renderer emits that prefix once.
- The device description is limited to 96 UTF-8 bytes. One fitting label is used
  exactly. For several labels, the lexicographically first is followed by ` +N`
  for the remaining unique labels. When the first label does not fit, truncation
  occurs at a complete UTF-8 rune, adds an ellipsis, and preserves its outer
  parentheses. The limit is Routevane's conservative bound, not a claimed
  Keenetic limit.
- Automatic RCI add and upsert commands include the native `comment` property.
  Removal identity remains prefix and interface only; a comment is never needed
  to delete an exactly owned route.
- The exact ownership ledger stores each output claim's full label set and
  compact description, and stores the effective description of a physical route
  Routevane created. Overlapping claims deterministically update that description.
  A migrated legacy claim with no labels cannot clear a previously known
  non-empty managed description.
- A pre-existing route is never edited merely because its prefix is desired.
  Generic deployment without a ledger remains additive and likewise leaves an
  existing same-prefix route and its description unchanged.
- Every ownership-aware write reads the `comment` field from
  `/rci/show/sc/ip/route`. Missing or different text fails verification and
  triggers the same verified-backup rollback as any other partial device write.

## Consequences

Descriptions identify the current category/list provenance without changing the
downloaded BAT contract. Renaming or regrouping a list changes the semantic plan
and updates descriptions only on routes that the exact ledger says Routevane
created.

Schema version 10 adds descriptions and claim-label JSON to the version 9
ledger. Legacy rows migrate to empty values instead of gaining invented
provenance.

The RCI `comment` property and the 96-byte product bound have not been accepted
on physical Keenetic hardware. Device doubles prove command construction,
read-back, mismatch rollback, overlap, and foreign-route preservation; physical
acceptance remains an explicit release risk.
