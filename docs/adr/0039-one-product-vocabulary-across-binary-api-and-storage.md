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

All four surfaces a version protects are involved: the local HTTP API, the
catalog format, the release archive layout, and — measured later, during the
identifier slice — the CLI flags, which name a list `--service` and
`--service-id` and log it under a `service` field.

## Decision

Adopt one vocabulary everywhere, and land it as a single breaking `0.2.0`.

The executable, its database, and its lock file are named `routevane`. The
mapping for the deferred identifiers is the interface vocabulary already adopted
by ADR 0028:

| Today | Becomes |
| --- | --- |
| `service`, `services` | `list`, `lists` |
| `list`, `lists` | `profile`, `profiles` |
| `category`, `categories` | unchanged |
| one entry, called a destination in prose | `rule`, as the code and interface already say |

This is a swap, not two independent renames: `lists` is both a source and a
target name. Every slice therefore renames `lists` to `profiles` first and only
then renames `services` to `lists`. Performed in the other order, the first step
walks into names the second step still needs, and the two meanings merge with
nothing left to tell them apart.

**`route` is not the word for a composition.** ADR 0028 named it «маршрут», and
that reads wrong to the operators this product serves: in every routing table a
route is one entry. It is also a claim the product cannot make. For the OpenWrt
nftset and Keenetic FQDN formats the artifact deliberately carries no address at
all, so `google.com` in a list is not a route but a name the device will route
by. The word stays free and keeps meaning what a device builds.

**Freeing `profile`.** The word is currently the rendering profile of a target:
`profile_key` in the target catalog, the `effective_profiles` table, and
`frozenProfiles` in code. That key means the renderer's version, so it is
renamed to `format_key` and the table to `effective_formats`. The rename is
worth doing on its own, and only target authors ever see the catalog key.

**One entry is a `rule`.** The interface and the planner already say so
(`plan.Rules`, `RuleKind`, «Одинаковое правило»); only the prose in `docs/`
still calls it a destination, and it is brought to the same word. Every target
ecosystem this product renders for calls the same thing a rule.

**Three wire names deliberately keep the retired word.** The routing plan JSON
(`internal/planjson`), the canonical payload the semantic plan hash is computed
over (`internal/planner/canonical.go`), and the plugin protocol
(`sdk/routevaneplugin`) all carry `service_id`, `profile_key` and `services`.
These are not internal identifiers. The plan JSON is the published `raw-json`
artifact and the bytes of every immutable plan snapshot; the hash payload
decides whether a rebuild produced the same plan; the protocol is what a
third-party renderer reads. Renaming their fields changes every plan hash, so
every stored output would republish on upgrade for no change in content, and it
breaks third-party plugins. That is a behavioural decision with its own release
note, and this ADR's own rule is that a slice needing one stops and takes it
separately. Their Go identifiers follow the current vocabulary while their tags
do not, which is the same split the retired catalog and transfer keys already
use. Renaming them, together with raising `RoutingPlanInterfaceVersion` from its
milestone-zero value, is separate work.

Storage names are renamed with the rest. Leaving them behind would preserve the
exact confusion this decision exists to end, inside the files that are hardest
to read without the translation table. The rename travels as one owned schema
migration, following the pattern `legacy_v3.go` already establishes.

The work lands as ordered slices, each independently green, all released
together:

0. **The interface dictionary.** «Маршрут» becomes «Профиль» and Route
   becomes Profile on every surface an operator reads. This supersedes the
   vocabulary clause of ADR 0028, which named the composition a route.
1. **Executable and its persisted state.** Directory, built binary, launchers,
   packaging, release tooling, CLI usage text, database filename, lock filename.
2. **HTTP API vocabulary and the web client that consumes it.** These cannot be
   separated: the client and the routes it calls change in one step. The
   browser's own addresses belong here too — they are bookmarkable and carry
   the same two meanings the API does.
3. **Catalog format keys.** `services:` becomes `lists:` in category files and
   `profile_key` becomes `format_key` in target files, with the reader
   accepting each retired key for one minor version.
4. **Go identifiers and SQLite storage**, as one migration.
5. **Configuration transfer format**, raising the version and keeping the
   documented ability to import the retired ones.
6. **CLI flags and the structured log field.** `--service` becomes `--list`,
   `--service-id` becomes `--list-id`, and the log field `service` becomes
   `list`. This was found while renaming the identifiers: the flags are the
   last thing an operator reads that still says service, and leaving them
   would keep the retired word in every documented example.

Renaming is not an occasion to change behavior. A slice that needs a behavioral
decision stops and takes it separately.

## Consequences

A stored database opened by the new version is migrated in place, and the
migration is not reversible by an older binary. The existing rule stands: an
older version is used with the backup taken before the update, never with a
migrated database.

An operator script that calls `./routing-agent` breaks and must call
`./routevane`, and one that passes `--service` must pass `--list`. Both are
stated in the release notes rather than absorbed by an alias, because a
compatibility shim would keep the retired name in the archive, in `--help`, and
in support conversations, which is the outcome being removed.

A bookmark of a retired browser address stops resolving. `/lists/{id}` cannot
redirect, because that address now belongs to the lists page rather than to the
route it used to name, and a shim would have to guess which of the two a visitor
meant. The break is stated in the release notes, for the same reason the
executable gets no alias.

The retired catalog key and the retired transfer versions are accepted for one
minor version so an operator's edited catalog and exported configuration survive
the upgrade. The retired database and lock filenames get no such grace: they are
internal, and the migration owns them.

One retired word is a stored value rather than a name: a recorded catalog
deletion says whether a category or a list was removed. The migration rewrites
every stored row, and the transfer reader accepts the retired value, because a
configuration exported by a retired version still carries it and the new schema
would refuse it.

Until every slice lands, the repository holds both vocabularies at once. The
interface dictionary stays the single place where the mapping is recorded, as
ADR 0028 established, and `docs/requirements.md` keeps stating the mapping until
the last slice removes the need for it.
