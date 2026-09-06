---
status: adopted
---

# ADR 0039: one product vocabulary across the executable, API, and storage

Completes the identifier work deferred by
[ADR 0028](0028-lists-live-in-categories-and-a-route-publishes-them.md) and
extends it to the executable and its persisted state.

## Context

The product is Routevane, but its executable is `routing-agent`. That name
reaches the operator directly: it is the file in the release archive, the
process in a task manager, the command in every CLI example, the database
`routing-agent.db`, and the lock `routing-agent.run.lock`. Every user-facing
document has to spend a sentence bridging the two names, and the troubleshooting
guide tells operators not to terminate an unidentified process while Routevane's
own process is the one that looks unidentified.

The second gap is older. ADR 0028 changed the interface vocabulary and deferred
the code and API identifiers to "a separate pure-refactor card once the surface
has settled". The surface has settled, but the deferred work is no longer a pure
refactor. The dual vocabulary now spans the local HTTP API, the catalog format,
SQLite table names, and the configuration transfer format. In the API, `services`
means what the interface calls lists, and `lists` means what the interface calls
routes. A reader of `docs/requirements.md` needs a translation table to follow
either one.

Measured footprint at the time of this decision:

| Surface | Extent |
| --- | --- |
| Executable name | 39 tracked files |
| Go identifiers | 171 files, 87 of them non-test |
| Web client | 47 files |
| HTTP routes | 60 references to `/v1/services`, 94 to `/v1/lists` |
| Catalog format | `services:` key in 13 category files |
| Storage | 12 tables and indexes carrying `service` or `list` |
| Transfer format | `config-transfer-v1.3` with a `services` key |

Three of the four surfaces a version protects are involved: the local HTTP API,
the catalog format, and the release archive layout. Only the CLI flags are
untouched.

## Decision

Adopt one vocabulary everywhere, and land it as a single breaking `0.2.0`.

The executable, its database, and its lock file are named `routevane`. The
mapping for the deferred identifiers is the interface vocabulary already adopted
by ADR 0028:

| Today | Becomes |
| --- | --- |
| `service`, `services` | `list`, `lists` |
| `list`, `lists` | `route`, `routes` |
| `category`, `categories` | unchanged |

This is a swap, not two independent renames: `lists` is both a source and a
target name. Every slice therefore renames `lists` to `routes` first and only
then renames `services` to `lists`. Performed in the other order, the first step
walks into names the second step still needs, and the two meanings merge with
nothing left to tell them apart.

Storage names are renamed with the rest. Leaving them behind would preserve the
exact confusion this decision exists to end, inside the files that are hardest
to read without the translation table. The rename travels as one owned schema
migration, following the pattern `legacy_v3.go` already establishes.

The work lands as ordered slices, each independently green, all released
together:

1. **Executable and its persisted state.** Directory, built binary, launchers,
   packaging, release tooling, CLI usage text, database filename, lock filename.
2. **HTTP API vocabulary and the web client that consumes it.** These cannot be
   separated: the client and the routes it calls change in one step.
3. **Catalog format key.** `services:` becomes `lists:` in category files, with
   the reader accepting the retired key for one minor version.
4. **Go identifiers and SQLite storage**, as one migration.
5. **Configuration transfer format**, raising the version and keeping the
   documented ability to import the retired ones.

Renaming is not an occasion to change behavior. A slice that needs a behavioral
decision stops and takes it separately.

## Consequences

A stored database opened by the new version is migrated in place, and the
migration is not reversible by an older binary. The existing rule stands: an
older version is used with the backup taken before the update, never with a
migrated database.

An operator script that calls `./routing-agent` breaks and must call
`./routevane`. This is stated in the release notes rather than absorbed by an
alias, because a compatibility shim would keep the retired name in the archive
and in support conversations, which is the outcome being removed.

The retired catalog key and the retired transfer versions are accepted for one
minor version so an operator's edited catalog and exported configuration survive
the upgrade. The retired database and lock filenames get no such grace: they are
internal, and the migration owns them.

Until every slice lands, the repository holds both vocabularies at once. The
interface dictionary stays the single place where the mapping is recorded, as
ADR 0028 established, and `docs/requirements.md` keeps stating the mapping until
the last slice removes the need for it.
