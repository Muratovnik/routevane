---
status: adopted
---

# ADR 0036: Resolve list overlaps by route-local priority

## Context

A route can combine many catalog lists, and large compositions routinely contain
hundreds of equal or contained destinations. Asking the operator to disable each
entry in the global library makes route setup impractical and changes every other
route using that list. Renderer-level deduplication also loses which list owns a
shared destination.

## Decision

Each route stores its resolved list ids from highest to lowest priority,
independently of whether a list was selected directly or through a live category.
Existing routes without stored priority use canonical id order. A list newly
carried by a category is appended; a list no longer carried disappears from the
resolved order without rewriting the stored category reference.

Before route labels and rendering, the planner assigns an equal destination to
the highest-priority owner. It also omits a lower-priority rule wholly covered by
a higher-priority rule. A broader rule from a lower-priority list remains when it
contains addresses not supplied by the higher list; priority never subtracts
networks or changes the requested routing union. Every omission keeps the
`lower_priority_overlap` reason in plan diagnostics.

The UI changes order by a drag handle and exposes the same action with the up and
down keys. Forecasts report relations found before assignment but count the
finished priority-resolved plan. They summarize automatic resolution instead of
showing a manual per-destination cleanup queue.

## Consequences

Priority is route-local, durable, and included in portable configuration v1.2.
Changing it rebuilds every output of that route. A service may contribute zero
owned rules after resolution and still remains part of the composition. Exact
ownership can change labels and diagnostics even when a renderer's destination
bytes would otherwise be identical.
