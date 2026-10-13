---
status: adopted
---

# ADR 0049: readable Keenetic FQDN group names

This decision replaces the naming paragraph of
[ADR 0040](0040-exact-fqdn-output-ownership.md). Ownership, the ledger and the
deployment lifecycle of ADR 0040 are unchanged.

## Context

ADR 0040 named a group `<prefix>-<output hash>-<list hash>-s<shard>`, for
example `routevane-db842808ecae-60965168ce762e94-s1`. On the router that name
says nothing about the list it routes. The name is what CLI commands and DNS
route attachments refer to; the native description that carries the list title
is a separate attribute, and whether a given firmware's web interface shows it
in place of the name is not established. Artifacts built before description
support carry no description at all, and an operator who delivered such an
artifact saw only the opaque names. A readable title beside an opaque identity
therefore does not make the groups reliably recognizable.

The list component does not need a hash. A list ID is already a slug of
lowercase ASCII letters, digits and hyphens, at most 63 bytes, which is a
command-safe name part. The output component is still needed: one router can
receive the same list from two outputs, and their groups must not share a name.

## Decision

New builds name a group `<prefix>-<list part>-<output code>-s<shard>`:

- **Prefix** keeps its ADR 0040 meaning: an optional per-output slug of at most
  24 bytes, default `routevane`.
- **List part** is the list ID, for example `chatgpt` or, for a custom list,
  `custom-<hex>`.
- **Output code** is the first 3 bytes of SHA-256 of the output ID as 6
  lowercase hex characters. It only tells apart two outputs on one router. It
  is not ownership evidence, as before.
- **Shard** is `-s1`, `-s2`, … and is always present.

An output whose ID hashes to `db8428…` therefore names its `chatgpt` group
`routevane-chatgpt-db8428-s1`.

A name is at most 64 bytes. The list part gets what remains after the prefix,
the two separators, the output code and the longest shard suffix the device can
hold, `-s128`. That is 42 bytes under the default prefix and 27 under a 24-byte
prefix. Reserving the longest suffix keeps one list part across every shard of a
list. A list too large for the device reserves room for its own shard count
instead, so its rule count can still be projected before the group budget
refuses it. A list ID longer than its budget is cut to fit, trailing hyphens are
removed, and `-` plus the first 4 bytes of SHA-256 of the whole list ID, as 8 hex
characters, is appended. Two long lists that share a head therefore keep
distinct names of the form `routevane-<33-byte head>-<8 hex>-<output code>-s1`.
The renderer still refuses a computed name over 64 bytes and a duplicate name
within one artifact.

The renderer ID and version stay `keenetic-fqdn-group` and
`keenetic-fqdn-group-v1`. Stored outputs and artifacts record the renderer
version. When it no longer matches, publication refuses to build the output and
no longer serves its artifacts. A version bump would therefore block every
existing Keenetic DNS output and make its published files unavailable. The command grammar is
unchanged and the validator accepts any command-safe group name, so artifacts
with the earlier names remain valid and deployable.

## Consequences

Existing groups are renamed on the next build and send of the output that owns
them. The new artifact no longer contains the earlier names, and the deployer
removes names recorded in that output's ledger that the artifact no longer
contains, as it already did for a prefix change. The send preview lists those
removals. Earlier names that no ledger records for that output, including
another output's groups and groups created by hand, are left untouched.

Custom lists show `custom-<hex>` in the name; their title remains in the
description. Renaming a list's title never renames its group, because the list
ID does not change.

The 24-bit output code can collide for two outputs that deliver the same list
to one router. The second output then finds an existing group it does not own,
and delivery stops with the ownership conflict of ADR 0040 instead of adopting
that group. Changing one output's prefix resolves it.
