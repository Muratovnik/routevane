---
status: adopted
---

# ADR 0044: bootstrap empty SQLite databases at a fixed schema version

## Context

A new database previously executed every historical migration before serving
its first request. Migration 13, which implements the storage vocabulary in
[ADR 0039](0039-one-product-vocabulary-across-binary-api-and-storage.md), renames
tables and columns throughout an already populated schema. SQLite follows each
rename through the other definitions. Profiling showed that this repeated
schema work dominated initialization under the race detector and could exhaust
the existing startup deadline even when the database contained no user data.

Existing databases still need those transformations. Their stored observations,
published artifacts, foreign keys, triggers, and column order must survive the
upgrade. The legacy importer also copies some rows with `INSERT ... SELECT *`,
so matching column names alone does not establish an equivalent schema.

## Decision

Use a fixed schema-15 checkpoint when creating a genuinely empty database.
The checked-in SQL is obtained from a database created by the historical
migration chain. It preserves the resulting definitions and column order.
It introduces no new persistent schema version or data format.

The production initializer may select this path only when `user_version` is
zero and `main.sqlite_schema` contains no objects. A view-only database and a
database containing internal schema objects are not empty. Both conditions
are checked again inside the transaction before creating the checkpoint.
A changed condition or a failed read is an error, not permission to continue
from the earlier observation.

The checkpoint's DDL, migration-history rows 1 through 15, and
`user_version = 15` commit in one transaction. The history records which schema
steps the resulting database includes; it does not claim that fifteen separate
transactions ran during this initialization. Errors or cancellation before
the checkpoint commits roll back that transaction.

`bootstrapSchemaVersion` remains fixed at 15. Existing versioned databases keep
the ordinary migration path, and later migrations above 15 execute through the
same path after a new checkpoint. Connection pragmas are configured and verified
before the checkpoint and pending migrations. The final schema verification
still runs after initialization. As before, a later migration or verification
failure does not undo an earlier successful commit.

Keep an explicit historical initializer for migration verification and failure
injection. Production selection does not depend on a test name, an executable
name, or a special test migration. Historical migration SQL stays unchanged.

## Verification contract

An independent historical replay and a production fresh open must produce the
same logical database. Compare all `sqlite_schema` definitions, including
indexes, automatic indexes, and triggers, together with table, column, index,
and foreign-key metadata. Physical page numbers and migration timestamps are
not logical-schema identities. Check the full history and any initialized data
separately.

The comparison must detect omissions that the ordinary startup checks do not
cover, such as removing a secondary index. Exercise constraints and immutable
publication triggers through writes as well as inspecting their definitions.
Checkpoint tests cover refusal of nonempty version-zero databases, changed
conditions inside the transaction, partial DDL and history failures, and
cancellation after work has begun.

The existing populated version-9, version-12, and legacy-v3 upgrade scenarios
remain required. They establish preservation of existing data and consumers;
a successful fresh-open comparison does not replace them. Reopening a fresh
checkpoint must also exercise the usual versioned-database path.

## Consequences

Fresh initialization avoids transformations whose purpose is upgrading old
data, while existing databases retain their migration behavior. The checkpoint
adds a second representation of one historical schema, so its comparison with
the independent migration chain is a required maintenance guard. Future schema
changes append migrations above the fixed checkpoint and remain covered by the
fresh-open and historical-replay comparison.

Startup and test time budgets remain unchanged. Performance measurements and
the complete race suite determine whether the initialization improvement is
sufficient; the checkpoint's existence alone is not that evidence.
