---
status: adopted
---

# ADR 0028: lists live in categories, and a route is what publishes them

> Current scope: The prohibition on deleting catalog categories below was replaced by ADR 0029.
> Vocabulary and category overlays remain; see [current UI contract](../UI.md).

## Context

The owner's review of the composer and the service card rejected the
object model the interface exposes. The surface speaks of three things — a
*category*, a *service* inside it, and a *routing list* that composes services
and publishes to connections — and the owner sees two: «есть категории, в
которые входят списки — всё». A "service" is only a convenient name for a set
of addresses; nothing stops the operator from putting other addresses into the
set called YouTube, so tying the set to a vendor is a false promise. The same
review found no way to create a category, no reason for a separate «Другие
сервисы» tab beside a category column that already lists members, and a
target chooser split into two blocks that read as two decisions.

Behind the vocabulary sits a data question: catalog categories are shipped as
YAML and were read-only, while the owner expects to add their own lists to
«Видео», create categories of their own, and have those changes reach every
route that names the category.

## Decision

- **Vocabulary.** What the interface called a *service* is a **list** (RU
  «список»): a named set of domains, addresses and networks, seeded by the
  catalog or created by the operator, editable in every case. What it called a
  *list* is a **route** (RU «маршрут»): a composition of categories and lists
  plus the connections it publishes to. A *category* keeps its name and now
  **contains lists**. «Списки» in the navigation becomes «Маршруты»; the
  composer builds «Новый маршрут»; the card is a list card with a «Состав
  списка» table. English mirrors this one-to-one: list, route, category.
- **Categories are operator-owned over a catalog seed.** The catalog still
  ships categories with their members; the operator's changes live in the
  store as an overlay — memberships added or removed on a catalog category,
  and whole categories the operator created. Every reader of category
  membership — the picker, the forecast, the planner's expansion of a route's
  categories — sees the merged result, so adding a list to «Видео» reaches
  every route that names «Видео» on its next rebuild, which is the point of a
  category. Catalog categories keep their title and cannot be deleted; an
  operator category can be renamed and deleted, and deletion is refused while
  a route names it, because a route that silently lost a category would build
  something its author did not choose.
- **No «Other services» tab.** Lists that belong to no category form the last
  category row, «Без категории», in the same column as every other; a list is
  found by search or by its category, never in a second surface.
- **One chooser for the connection format.** Routers and applications are
  groups inside one searchable combobox, not two blocks with two selections.
- **Identifiers follow later.** The interface vocabulary changes now; the Go
  and API identifiers (`services`, `lists`, `categories`) are renamed in a
  separate pure-refactor card once the surface has settled, so this decision
  does not collide with the primitives and validation work landing in the same
  cycle.

## Consequences

The planner's category expansion becomes a read through the overlay rather
than a lookup in the loaded catalog; semantic hashes still describe the
expanded services, so a membership change changes the next build and nothing
else. The store gains one more schema step with two tables and the same
write-through registry pattern operator services use (ADR 0024). Two words now
mean different things in the code and on the screen until the follow-up
rename lands; the dictionary is the single place where that mapping lives.
