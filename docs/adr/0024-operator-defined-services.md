---
status: adopted
---

# ADR 0024: an operator-defined service is a catalog entry, not a list note

> Current scope: The original per-list editing split was replaced by ADR 0025 and the separate
> library flow in ADR 0029. See [current UI contract](../UI.md).

## Context

Composition could only name what the shipped catalog carries. An operator whose
service is not in the catalog had no honest path: the per-list domain override
(schema v3) corrects a shipped service for one list, but it cannot introduce a
service, and editing a YAML file inside the installation directory is not an
interface. The owner named this the missing half of the product: "есть только
готовые сервисы".

Two shapes were considered. A *list-local* set of extra domains keeps storage
small but creates a second kind of composition entry that has no identity: it
cannot be reused by a second list, cannot be shown in the picker, and cannot be
excluded by a category reference. A *catalog* entry behaves like every other
service everywhere — picker, composition, resolution, refresh, build,
diagnostics — at the cost of a stored registry.

## Decision

- **A custom service is a global catalog entry stored in SQLite** (schema v4,
  `custom_services`): an identity, a title, and 1–64 normalized domain
  suffixes. It has no automatic sources; what the operator wrote is the whole
  definition.
- **Identity carries the reserved `custom-` prefix** and is generated, never
  operator-supplied. The shipped catalog cannot legally use the prefix, so a
  later catalog import can never collide with a stored service.
- **The planner sees it as an ordinary definition.** Its domains become manual
  `domain_suffix` seeds of one required component, and it carries the shipped
  catalog's revision, because the planner accepts exactly one revision per
  plan and a custom service must compose with the catalog it extends — a
  revision of its own would make every mixed list unbuildable. Freshness does
  not depend on the revision: the domains enter every plan straight from the
  definition, and an edit reaches the artifact through the plan's semantic
  hash on the next rebuild.
- **Every definition lookup goes through one accessor** that merges the shipped
  map with an in-process registry hydrated from the store at startup and kept
  write-through. The process lock already guarantees a single writer, so the
  registry cannot drift while the process lives.
- **Title and domains are mutable; identity and creation are not, and there is
  no delete.** A list referencing a vanished service would publish less than it
  says — the same argument as ADR 0004. The change reaches subscribers on the
  next refresh-and-rebuild, exactly like a shipped catalog edit.
- **Over HTTP the catalog collection accepts POST** (`POST /v1/services`,
  `POST /v1/services/{id}/update`), and the listing marks the entry
  `custom: true` so the surface offers editing only where a stored row exists.

## Consequences

The picker offers one more source of services and one dialog in two modes: a
shipped service edits its list-local override, a custom service edits itself.
Categories remain catalog-only (ADR 0016) — a custom service joins lists
directly and appears with the uncategorized services.

The registry is bounded (128 entries, 64 domains each) by the same constants
that bound a composition, so the one-binary product gains no unbounded stored
surface. CLI `build`/`refresh` by service id continue to read only the YAML
catalog; custom services live behind the serving process that owns the store.
