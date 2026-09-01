---
status: adopted
---

# ADR 0029: composing is per route, and the library is its own flow

## Context

The preceding library change gave the operator categories they own and a list
card they can edit. The owner's review of the result (2026-08-29) rejected
where that editing happens:

> «Меня смущает фраза "Правки здесь действуют во всех маршрутах". Это странно.
> Я же на экране сборки маршрута, а меняю сразу все. Это же отдельный флоу
> должен быть.»

and, asked what deleting a catalog object should mean:

> «Удаление на этапе сборки маршрута, опять же, не должно быть глобальным
> действием. Мы можем вполне удалить именно из этой сборки, а другие просто
> оставить включенные или отключенные. Глобальное редактирование — отдельный
> флоу.»

The caption «Правки здесь действуют во всех маршрутах» (ADR 0027) was an
admission, not a design: a surface that has to warn about its own reach has the
wrong reach. The same confusion produced the rest of the review — a bin glyph
on a list row that meant "remove from category" (a global write) beside a
checkbox that meant "not in this route", and no way to delete a category at
all, because the overlay could add and remove membership but never remove a
category the catalog shipped.

A per-route escape looked available: the composition carries `service_domains`,
which the store, the planner and the forecast all read. It turned out not to be
one — see the decision below — and finding that out in the running product,
rather than in the code that called it, is why this ADR records it.

## Decision

- **Two flows, and the surface says which one it is by being that surface.**
  Composing a route (`/lists/new`, the route page's Contents tab) writes
  nothing global. Curating the library — what a list holds, where its entries
  come from, which category holds it, and whether a list or a category exists
  at all — happens in a section of its own, «Списки» (`/library`). No caption
  explains the reach of an edit, because no surface has two reaches.
- **The list card has two modes.** Opened while composing, it *reads* the list:
  its entries are shown, the footer states and toggles membership in this
  route, and «Открыть в библиотеке» leads to the other flow in a new tab. It
  writes nothing — the route's own act is the footer, and the list itself is
  edited where lists are edited. Opened in the library, it edits the list —
  rename, entries, sources, refresh, delete — and has no footer, because no
  route is in question.
- **A route cannot yet drop one entry of a list.** The first draft of this
  decision gave the composing card a per-row switch writing the composition's
  `service_domains` override. That was wrong twice over, and the product said
  so when it ran: the override replaces only catalog domain seeds, so an
  observed rule switched off would still be published, and it accepts domains
  alone, at most 64, so a real list of 91–188 entries including addresses and
  networks was refused — `POST /v1/lists/preview` answered 422 and the capacity
  forecast went silent. Per-route control therefore stays at the level the
  product can honour: a route includes or excludes a whole list. A real
  per-route exclusion of individual values — subtracting rather than pinning,
  covering all three destination kinds, honoured by the planner and the
  forecast — is its own slice and is not in this one.
- **The operator owns the library, including what the catalog shipped.**
  A category or a list can be deleted whoever created it. For a catalog object
  the deletion is a removal record in the overlay: the shipped catalog file is
  never edited, and a catalog update does not resurrect what the operator
  removed. Deleting a category asks one question — move its lists to «Без
  категории», or delete them with it.
- **Deletion never rewrites a stored route.** Removing a list or a category a
  route names directly is refused, naming those routes, exactly as removing a
  category in use already was. Removing a list from a category *is* allowed to
  change what a route carries: that is what naming a category means, and it is
  the same act as adding one (ADR 0028).
- **Selection is a checkbox and nothing else.** The picker while composing has
  no category menu, no «Своя категория», no «Свой список» and no per-row bin:
  those are library acts. It has search, checkboxes, and a chevron that opens
  the card.

## Consequences

- SQLite schema v7 adds one table of removals keyed by kind and id. Removing an
  operator-created category or list deletes its row instead; removing a catalog
  one records a removal. Every catalog read subtracts removals before the
  merge, so one accessor keeps the planner, the forecast, the picker and the
  library agreeing.
- `POST /v1/categories/{id}/remove` takes `{"lists":"detach"|"delete"}` and
  accepts catalog categories; `POST /v1/services/{id}/remove` is new. Both
  answer 409 with the routes that would break.
- The card's per-row switch exists in one mode only. A control that means two
  different things depending on where it was opened is a control that has to be
  read twice; here the second meaning also could not be delivered, which is the
  stronger argument for keeping the switch where it acts.
- A stale catalog is now possible while both flows are open at once. The
  composing screen re-reads the catalog when its window regains focus rather
  than holding a copy from before the operator's library edit.
- The follow-up rename of Go and API identifiers (ADR 0028) now also owes a URL
  for the library: `/library` is deliberate and survives the rename, while
  `/lists/{id}` still addresses a route until then.
