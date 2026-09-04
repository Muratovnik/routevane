---
status: adopted
---

# ADR 0036: Initialize route-local list priority from a library default

## Context

A route can combine many catalog lists, and large compositions routinely contain
hundreds of equal or contained destinations. Asking the operator to disable each
entry in the global library makes route setup impractical and changes every other
route using that list. Renderer-level deduplication also loses which list owns a
shared destination. New routes still need a predictable starting order without
making one library order a live policy for every existing route.

## Decision

The library stores one configurable default order over its complete list set.
Creating or forecasting a route with omitted or empty priority filters that
default to the resolved composition and appends any new catalog list not yet in
the preference. An explicit non-empty route priority always wins.

Each created route stores its resolved list ids from highest to lowest priority,
independently of whether a list was selected directly or through a live category.
That snapshot does not change when the library default is reordered. Existing
routes without stored priority retain their canonical-id fallback. A list newly
carried by a category is appended after the saved route order; a list no longer
carried disappears from the resolved order without rewriting the stored category
reference.

Before route labels and rendering, the planner assigns an equal destination to
the highest-priority owner. It also omits a lower-priority rule wholly covered by
a higher-priority rule. A broader rule from a lower-priority list remains when it
contains addresses not supplied by the higher list; priority never subtracts
networks or changes the requested routing union. Every omission keeps the
`lower_priority_overlap` reason in plan diagnostics.

The UI changes both the library default and a route's own order by a drag handle
and exposes the same action with the up and down keys. It communicates priority
by position rather than visible ordinal labels. Forecasts report relations found
before assignment but count the finished priority-resolved plan. Their complete
per-list summary drives compact intersection tags naming every other selected
list with any relation. Detailed diagnostic records may be capped at 100; the
summary is not. The composer therefore summarizes automatic resolution instead
of showing a manual per-destination cleanup queue.

## Consequences

The library default and every route-local priority are durable and included in
portable configuration `config-transfer-v1.3`; v1.2 route priorities remain
readable. Changing the default affects only later omitted/empty route priorities.
Changing a stored route's order rebuilds every output of that route. A service
may contribute zero owned rules after resolution and still remains part of the
composition. Exact ownership can change labels and diagnostics even when a
renderer's destination bytes would otherwise be identical.
