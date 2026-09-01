---
name: routevane-go-slice
description: Implement or review one Routevane Go backend milestone or vertical slice, including domain, planner, source, renderer, persistence, HTTP, or device boundaries. Do not use for frontend-only work.
---

# Routevane Go slice

Read `AGENTS.md`, `docs/requirements.md`, and only the routed rule files that
the slice touches. Historical milestones are context, not current setup instructions.

Define the user-visible outcome, forbidden later-milestone scope, data and
network boundaries, rollback, and the end-to-end oracle before editing. Inspect
current packages first; create only packages the slice immediately consumes.

Keep policy decisions in the planner, format decisions in a renderer, device
limits in a target profile, and deployment in a deployer. Preserve provenance,
reason codes, deterministic ordering, semantic hashing, and stale-observation
exclusion at every boundary.

Add the smallest tests that prove the vertical outcome: table tests for cases,
fuzz/property tests for actual parsers and invariants, golden tests for formats,
and one runnable end-to-end command. Run `tools/dev.ps1 check`, then run the
slice's visible command against throwaway state. Report the exact artifact and
oracle; do not call package coverage alone acceptance.
