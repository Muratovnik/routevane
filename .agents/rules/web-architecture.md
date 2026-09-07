# Routevane web architecture

Routed from `AGENTS.md`. Read this before changing `web/src`.

Use Feature-Sliced layers from high to low: `pages`, `widgets`, `features`,
`entities`, `shared`. `app.vue` and global styles exist once. A file imports
only a lower layer; cross-slice imports use the `@/` alias. Do not create empty
layers or index barrels without a current consumer. `eslint-plugin-boundaries`
enforces that order, including the refusal of a same-layer import between two
slices; entities present what they are given and do not import `shared/api`,
which features orchestrate.

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
