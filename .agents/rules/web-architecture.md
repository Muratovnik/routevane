# Routevane web architecture

Routed from `AGENTS.md`. Read this before changing `web/src`.

Use Feature-Sliced layers from high to low: `pages`, `widgets`, `features`,
`entities`, `shared`. `app.vue` and global styles exist once. A file imports
only a lower layer; cross-slice imports use the `@/` alias. Do not create empty
layers or index barrels without a current consumer. `eslint-plugin-boundaries`
enforces that order, including the refusal of a same-layer import between two
slices. An entity's `ui` presents what it is given and never imports
`shared/api`, at error level: the requests a slice makes, and the state they
read and write, live in that slice's `model`, which the component takes its
state and actions from.

Inside a slice a `.vue` file is a component and every other file is named for
its one role: `model/useX.ts` is a composable and the state it owns, `types.ts`
is a type contract a host imports without importing a component, and `lib/x.ts`
is pure functions. There is no separate store layer — state lives in a
composable, and state the page rather than one caller owns is a module-scope
`ref` in that composable's module. A component whose own state is the page's
rather than one instance's keeps it in a plain `<script lang="ts">` block beside
its `<script setup>`, which is evaluated once per module.

A function is an arrow expression, and the `function` keyword appears only
where an arrow cannot express the same thing — a class, or an Options-API stub
that needs `this`. A module-level constant whose value is a literal is
UPPER_CASE.

Styling is semantic tokens plus named classes. Raw palette, typography,
spacing, radius, shadow, and motion values are declared in
`src/assets/styles/tokens.css`; component code consumes semantic variables.
There is no utility CSS framework. A component's styles live in its own
`<style scoped>` block, the only stylesheets outside a component are the global
ones in `src/assets/styles`, and `<style src>` is not used.

Unit specs live in `web/tests/unit/` mirroring `src/`, the browser suites live
in `web/tests/{e2e,desktop,dev,update}`, and `src/` holds no test file.
`web/tests/unit/structure.spec.ts` enforces that placement and the stylesheet
placement above.

A unit spec renders through `vitest-browser-vue` in the pinned Chromium and
queries by role, accessible name, label, or text; `data-testid` is for an
element none of those identifies, which in practice means one that is
deliberately `aria-hidden`. DOM ids stay accessibility bindings — `label for`,
`aria-labelledby`, a field's `input-id` — and are never test hooks. A spec keeps
no mutable module state: it builds a scenario per case, awaits deferred work
through `Promise.withResolvers`, and counts calls on a `vi.fn` record.
`vitest.config.ts` names the specs that run in Node instead; everything else
runs in the browser, because that is where a document, browser storage and the
service-origin lookup actually behave.

The browser suites are split by user-visible area, with the product boot and
the shared queries in `web/tests/e2e/support`: a suite names the data directory
the worker fixture serves it and states nothing about starting, proving or
stopping the product. A suite locates an element the way an operator perceives
it — by role, accessible name, label, or the words on screen — and uses
`data-testid` only where none of those identifies it.
`playwright/no-raw-locators` gates that, and the few selectors it allows are
written once in `support/queries.ts`, each with the reason it has no accessible
handle. A test states what it set up instead of probing for it: the expectation
belonging to one case lives in that case's row of the parameter table, the
processes and files a test creates are torn down by a fixture rather than by a
branch in `finally`, and a wait names the observable condition it waits for —
gated by `playwright/no-conditional-in-test`, `no-conditional-expect`,
`no-wait-for-timeout` and `sonarjs/no-fixed-wait-in-tests`.

A component is a black box. A host styles its own root class, passes props,
places slot content, or supplies a documented custom property. It does not
reach through another component with element or implementation selectors.

Every product state has loading, empty, error, stale, and success behavior as
applicable. Use native interactive elements, visible focus, logical headings,
labels, keyboard traversal, and non-color state text. Follow the desktop layout
and accessibility checks in [the UI contract](../../docs/UI.md#desktop-layout-and-window-adaptation).
Constrained windows and enlarged content must remain usable; they do not imply
a separate mobile interface. Axe automation blocks serious and critical findings
but does not replace keyboard and screen-reader review.

Nuxt output is generated, not committed, and embedded by the canonical Go build.
The build path and served UI digest are one tested contract. A Nuxt development
server alone is not acceptance of either the CLI's embedded interface or the
Electron desktop package. Desktop acceptance runs the packaged application.
