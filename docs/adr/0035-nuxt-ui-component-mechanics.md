---
status: adopted
---

# ADR 0035: Nuxt UI supplies interactive component mechanics

## Context

Routevane had accumulated local interaction code around headless primitives.
The result was visually consistent, but common behavior still diverged: a
disabled link could retain hover feedback, loading could show two indicators,
and the side sheet had no perceptible entrance. Rebuilding these mechanics per
control duplicates work already maintained and accessibility-tested by a
component library.

The application is a generated Nuxt SPA with an established token system,
light and dark modes, no remote font dependency, and stable `Rv*` component
interfaces used by feature code. A library therefore has to support Nuxt and
Vue directly, preserve client-only generation, expose styling slots, and allow
incremental adoption without replacing the visual system.

Nuxt UI 4.11.0 fits those boundaries and uses the same Reka UI foundation
already present in Routevane. PrimeVue and Vuetify offer broader suites but
would introduce a second, more prescriptive visual system and a larger
migration surface. Continuing with only headless Reka would leave Routevane
responsible for the styled loading, disabled and transition behavior this
decision is meant to stop owning.

## Decision

- Nuxt UI 4.11.0 is the maintained styled component layer. Its direct
  `reka-ui` dependency is aligned to the single version selected by Nuxt UI.
- Feature and page code continues to import only Routevane `Rv*` facades.
  Library `U*` components may appear only inside `web/src/shared/ui` and the
  application root required by Nuxt UI.
- `RvButton` delegates element selection, disabled-link behavior and loading
  mechanics to `UButton`. `RvDialog` delegates its full-height sheet structure,
  focus mechanics, dismissal, scroll locking and ordinary sheet transitions to
  `USlideover`. The bounded centred panel remains
  on the existing Reka dialog during incremental migration.
- The current Nuxt UI 4.11.1 / Reka UI 2.10.4 sheet uses the library's stateful
  keyframes. Reka assigns `animationFillMode` through CSSOM; that assignment is
  permitted by the application's `style-src 'self'` policy. The former rationale
  equated it with replacing a style attribute, which the policy does block.
  A workspace inspection pane disables the sheet's own animation because its
  enclosing workspace already animates a View Transition snapshot: enabling both
  translates the live pane again after the snapshot settles. This exception
  preserves one transition owner and immediate removal during workspace changes;
  it is not a CSP workaround. USlideover still owns focus return, dismissal and
  scroll locking in that case.
- Nuxt UI's font and color-mode modules are disabled. Routevane continues to
  own local font stacks, theme selection and all `--rv-*` semantic tokens. A
  small CSS bridge maps Nuxt UI roles to those tokens; feature code does not
  use utility classes or library colors.
- The Nuxt UI runtime color plugin is removed after its module registers. It
  creates two inline style elements in a client-only SPA, while Routevane's
  palette is already static and its CSP allows styles only from packaged
  files. The CSS bridge therefore defines the library's primary role as well
  as its neutral surface roles.
- Adoption is incremental. A raw control is replaced when its owning flow is
  changed and the facade has regression coverage; installing the library does
  not authorize an unrelated whole-interface rewrite.
- `RvDisclosure` retains its small local state adapter and CSS grid transition.
  Reka Collapsible 2.10.4 can retain content with `unmountOnHide=false`, but then
  hides it with `hidden="until-found"` and reopens it on browser find. With
  `forceMount` it leaves hiding to the caller. Routevane requires retained draft
  state with immediate inertness while closing, so either route still needs
  the existing visibility adapter; the library additionally measures dimensions
  that the grid transition does not use. This bounded replacement adds no
  required behavior. Reconsider it if browser-find expansion becomes a product
  requirement or a shared library mode can own the entire visibility contract.
- Route priority dragging uses VueUse's maintained SortableJS integration.
  Routevane owns the row styling and an equivalent keyboard action; SortableJS
  owns pointer/touch reordering and its drag lifecycle.

## Consequences

Disabled and loading semantics and the side-sheet motion now follow maintained
library behavior while Routevane retains one product-specific visual API. The
dependency adds build size and requires compatibility checks during Nuxt UI or
Reka upgrades. A rollback removes the Nuxt module and restores the internals of
the affected `Rv*` facades without changing feature callers.

Buttons and full-height side sheets use Nuxt UI. Tabs delegate their complete
tab/retained-panel relationship to Reka, including keyboard traversal and panel
focus entry. Other facades remain valid and are not considered migrated merely
because Nuxt UI is present.
