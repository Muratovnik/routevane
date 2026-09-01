---
status: adopted
---

# ADR 000: use the supported Nuxt major

## Context

The attached implementation plan names Nuxt 3 for the UI milestone. Official
Nuxt documentation marks Nuxt 3 unsupported after 2026-07-31. Nuxt 4.5.2 is the
current stable release as of the repository bootstrap on 2026-08-20.

Starting a new security-sensitive local application on an unsupported major
would make the scaffold stale on day one and force a migration before product
work begins.

## Decision

Use Nuxt 4.5.2 with Node 24 and keep the UI contract framework-light: Vue,
TypeScript, semantic CSS tokens, unit tests, and browser accessibility tests.
No Nuxt-specific product code belongs in the Go domain.

## Consequences

- The plan's user flows and milestone boundary remain unchanged.
- Nuxt 3 directory and data-fetching assumptions are not copied.
- The exact npm graph is committed in `web/package-lock.json`.
- If a required module proves incompatible, rollback is a single frontend-only
  commit to Nuxt 3.21.11, with the explicit acceptance that the line is EOL.

## Sources

- https://nuxt.com/docs/4.x/community/roadmap
- https://github.com/nuxt/nuxt/releases/tag/v4.5.2
- https://nuxt.com/docs/3.x/getting-started/installation
