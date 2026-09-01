---
name: routevane-ui-slice
description: Build or review a Routevane Nuxt operational UI slice, page, component, state, or design-system change. Do not use for backend-only work or image-only exploration.
---

# Routevane UI slice

This is the complete Routevane-specific workflow for implementation and review
of the web UI. Do not depend on an undeclared user-scoped UI skill.

Read `AGENTS.md`, `docs/UI.md`, `.agents/rules/web-architecture.md`, and
`docs/requirements.md`. Keep the automatic user path primary and diagnostics
progressive. Reuse semantic tokens and existing primitives before adding one.

Implement real loading, empty, error, stale, success, keyboard, focus, and
narrow-screen behavior relevant to the slice. Keep data fetching outside
presentational components and keep product state in the URL or server state
when it must survive refresh or be shareable.

Run `npm run check` from `web/`. For user-visible changes also run
`tools/dev.ps1 test-browser`, restart the server that owns the built asset, and
inspect the rendered result at 320, 768, 1024, and 1440 pixels. Axe is a gate,
not a substitute for keyboard and visual review.
