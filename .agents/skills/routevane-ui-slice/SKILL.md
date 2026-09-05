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
constrained-window behavior relevant to the slice. Keep data fetching outside
presentational components and keep product state in the URL or server state
when it must survive refresh or be shareable.

For implementation, run the verification commands in `AGENTS.md`, including
`tools/dev.ps1 test-browser` for browser-facing changes. For user-visible changes,
inspect the rendered result using the desktop, constrained-window and
accessibility checks in [the UI contract](../../../docs/UI.md#desktop-layout-and-window-adaptation).
Axe is a gate, not a substitute for keyboard and visual review.

Follow `AGENTS.md` for live acceptance: restart a server only when it belongs to
this task, after confirming ownership and a rollback path. Otherwise launch an
isolated task-owned process and clean it up afterward. Preserve another task's
server and generated working directories; use an isolated checkout when checks
would interfere with an active development session.
