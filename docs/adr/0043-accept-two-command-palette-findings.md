---
status: adopted
---

# ADR 0043: accept two command palette accessibility findings

## Context

Routevane's searchable choices — `RvSearchSelect` at field and compact size,
and `RvCombobox` through it — are drawn by Nuxt UI (ADR 0035). The library
offers two components for a choice with a search field, and neither passes
every rule the browser suites hold a screen to.

`USelectMenu` in Nuxt UI 4.11.2 with Reka UI 2.10.4 places its search field
inside the element that carries `role="listbox"`, which axe 4.13 reports under
`aria-required-children` (critical), and the scrolling region of a long list
holds no focusable content, which it reports under
`scrollable-region-focusable` (serious).

`UCommandPalette` in a `UPopover` keeps its search outside the list and passes
`aria-required-children`. Two findings remain:

- Its list is Reka's `ListboxContent`, rendered without an accessible name and
  without passing on any attribute that could give it one, so axe reports it
  under `aria-input-field-name` (serious, WCAG 4.1.2). The list is announced
  inside a named panel and its search field is labelled.
- A list longer than its panel scrolls in the palette's viewport. The options
  take no focus of their own: the search field keeps the focus and names the
  highlighted option as its `aria-activedescendant`, and the arrow keys move
  that highlight and scroll it into view. axe does not follow the active
  descendant and reports the viewport under `scrollable-region-focusable`
  (serious, WCAG 2.1.1). Every row is reachable from the keyboard.

The owner decided to draw the searchable choices with the command palette and
accepted these two findings while the library structures the palette this way.

## Decision

- The browser suites run axe through one helper, `web/tests/e2e/support/axe.ts`.
  Every rule stays enabled. A node is waived only when it fails only the checks
  of the accepted rule, appears under no other rule, and lies inside a panel
  carrying `data-rv-choice-panel="palette"`, which only `RvSearchSelect` sets:
  - `aria-input-field-name` on the palette's `ListboxContent`, directly inside
    the palette root, while the panel has an accessible name and the palette's
    search field is labelled;
  - `scrollable-region-focusable` on that list's `data-slot="viewport"`, while
    the search field's `aria-activedescendant` names an option of that list.
- A listbox or a scrolling region anywhere else is reported as before, and so
  is the palette's own element once the condition it was waived on no longer
  holds.
- Browser tests prove both sides. An open palette passes the audit; a nameless
  listbox outside the palette and a second listbox inside its panel fail it, as
  does the palette's list once its panel loses its name. On a list of 32
  options the arrow keys reach, highlight and scroll to the last row while the
  focus stays in the search field, the audit passes, and it fails once an
  unmarked scrolling region is added or the search field stops naming an active
  descendant.
- `USelectMenu` is not used, because of its `aria-required-children` finding
  and the same scrolling one.

## Consequences

Assistive technology meets a listbox without a name of its own inside a named
panel, and a scrolling list whose rows are reached through the search field
rather than by focusing the list. Both are the library's structure, not a
Routevane defect a facade could correct. Every other accessibility finding,
including any other finding on the same panel, still fails the suites.

Revisit this decision, and remove the waivers, when Nuxt UI passes attributes
on to the palette's `ListboxContent` or gives that list a name, or when the
palette makes its viewport or its rows focusable. A library upgrade reruns the
tests that show whether each waiver still matches anything.
