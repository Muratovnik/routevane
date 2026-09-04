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

`config-transfer-v1.0` exports strict canonical JSON from one read snapshot.
Operator-owned records use deterministic transfer-local references; catalog
references remain catalog ids. Applying a previewed document only succeeds on a
fresh database and creates fresh persistent identities in one transaction.

The transfer excludes credentials, tokens, hashes, publication pointers,
artifacts, history, observations, browser preferences, caches, backups, audits,
and timestamps. Device metadata may transfer, but credentials and automatic
delivery do not; output publication and subscription state remain empty.

A destination must already carry every referenced catalog target and may not
depend on a `catalog/local` service. Target metadata is re-derived from that
destination's catalog and renderer registry.

## Consequences

The transfer is intentionally not a backup or a migration path for an existing
installation. Operators preview before apply and re-enter device credentials
before enabling automatic delivery. A future schema revision requires a new
versioned document contract rather than silently accepting extra fields.
