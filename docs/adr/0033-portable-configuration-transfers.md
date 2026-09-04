---
status: adopted
---

# ADR 0033: Portable configuration transfers use fresh identities

## Context

An operator needs to move authored Routevane configuration to a new machine
without moving credentials, published files, observation history, bearer
subscriptions, or source-machine database identities. Reusing SQLite ids would
also make a transfer an accidental persistence protocol rather than a portable
configuration document.

## Decision

`config-transfer-v1.1` exports strict canonical JSON from one read snapshot.
Operator-owned records use deterministic transfer-local references; catalog
references remain catalog ids. Applying a previewed document only succeeds on a
fresh database and creates fresh persistent identities in one transaction.

The transfer excludes credentials, tokens, hashes, publication pointers,
artifacts, history, observations, browser preferences, caches, backups, audits,
timestamps, and operator-added HTTP sources. A source URL can contain a
credential in its host, path, or query, so no URL-shape heuristic makes it safe
to export. The document records only how many custom sources were omitted and
preview warns that they must be recreated. A disabled reference to an omitted
source is omitted with it; enabled/disabled choices for catalog sources remain
portable. A v1 document containing a custom source URL is refused.
The importer still recognizes a legacy `config-transfer-v1.0` document only
when it has neither a custom source URL nor the v1.1 omission field. This lets a
non-sensitive old file move forward while old importers fail cleanly on v1.1
rather than silently interpreting a changed schema.

Device metadata may transfer, but credentials and automatic delivery do not;
output publication and subscription state remain empty. Preview and apply
receive the same raw UTF-8 JSON document. The preview digest travels in the
`X-Routevane-Transfer-Digest` request header during apply, so an envelope cannot
rewrite duplicate keys or reduce the document's own size budget. Duplicate keys
at any depth are refused. Export, preview, apply, and the browser share a 64 MiB
limit. This covers the current stored maximum — including 200 routes with up to
512 list-local domains each and 16,384 membership/verdict rows — with headroom;
export refuses a document above it instead of creating an unusable file.

A destination must already carry every referenced catalog target and may not
depend on a `catalog/local` service. Target metadata is re-derived from that
destination's catalog and renderer registry.

## Consequences

The transfer is intentionally not a backup or a migration path for an existing
installation. Operators preview before apply and re-enter device credentials
before enabling automatic delivery. They also recreate operator-added HTTP
sources after transfer. A future schema revision requires a new versioned
document contract rather than silently accepting extra fields.
