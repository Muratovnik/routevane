---
status: adopted
---

# ADR 0013: the list is the product unit; outputs bind formats and devices

## Context

The adopted plan's §1 scenario is device-first: the user picks a target device,
then services, and the stored profile permanently binds that one target. Core
MVP, milestone 5 and the 2026-08-21 library interface all implement that unit.

The owner's acceptance review on 2026-08-22 rejected it. The base scenario is
list-first (the iplist.opencck.org shape): assemble a list of services — from a
ready-made preset or by hand — and only then decide what to do with it:
download it in some format, expose a subscription URL, or push it to a router
through an adapter. Most users operate one device and are confused by a target
question inside list creation; users with several devices need per-device
version management, not per-device copies of the same list.

Plan §3.8 already separates the canonical decision from its renderings — one
PlanSnapshot renders into several ArtifactBuilds — but the profile's baked-in
target keeps that separation out of the API and the interface. Three models
were considered: (A) device-first-class with mandatory list↔device attachments,
(B) a default-device preference over the current schema, and (C) target-free
lists with outputs that optionally name a device. A is C with the device made
mandatory, and it cannot express "just a file and a subscription URL", which is
the dominant case until the milestone 8 deployer exists. B cannot let one list
serve two devices. The owner chose C.

## Decision

- The unit of the product is the **list**: a named, editable set of services
  with no target. Renaming and changing composition are forward edits; every
  rebuild produces new immutable PlanSnapshot and ArtifactBuild rows, so the
  version history of what was actually published is preserved unchanged
  (ADR 0004 stays intact — its create-only profile rule was scoped to M3).
  Saving a composition change rebuilds the list's outputs, so a published file
  never silently disagrees with the list it came from. Editing and building
  stay separate operations, because renaming a list must not reach the network
  and a rebuild must: the surface performs both on one save, and that is where
  the guarantee is kept. A list's name is proposed — from the preset, or from
  the picked services — and stays editable; naming is never a gate on creating
  a list.
- An **output** joins one list to one renderer format and optionally to one
  registered device. The output owns its subscription URL and its artifact
  version history. An output without a device serves downloads and the
  subscription; an output with a device additionally carries delivery
  (milestone 8 deployer, backup, rollback, delivered-version tracking).
- A **device** is a user-registered instance of a catalog target: kind and
  format come from the catalog; name and connection settings from the user.
  The catalog remains a read-only library of supported formats.
- **Presets** are catalog-shipped ready-made service bundles. Instantiating a
  preset creates an ordinary editable list; presets are starting points, not a
  separate object kind. By default the new list is a copy and the preset stops
  mattering. *(Superseded by ADR 0016: a preset is a category, and taking one
  is a reference rather than a copy.)*
- A list may instead **follow** its preset, and following is hybrid rather than
  all-or-nothing. Composition is then `preset ∪ added − excluded`: the list
  stores the preset reference, the services the user added, and the services
  the user removed from the preset's part. Removing a followed service records
  an exclusion and keeps following; it never silently detaches the list. A
  service the preset gains later is added and rebuilt automatically, and the
  list's history records that the preset — not the user — added it.
  *(Superseded by ADR 0016: following is not a mode. Every list holds its own
  entries plus one-level references and exclusions, and deduplication happens
  at the output.)*
- **Refresh** is scheduled: a service-wide default interval in settings, which
  any single list may override with its own. Manual is the default everywhere,
  so nothing reaches the network on a timer until the operator says so.
- An **output that names a device** may deliver unattended, which requires the
  device credential to outlive one attempt. That is a security change and is
  decided separately in ADR 0014, which replaces ADR 0012.

## Consequences

- The `profiles` schema and `/v1/profiles` API are reshaped by migration: the
  target moves off the profile onto a new outputs entity. Each existing profile
  migrates to one list plus one output carrying its target, subscription and
  artifact history — existing subscription URLs keep serving and no token is
  rotated (ADR 0004).
- The interface follows the unit: the composer asks only about services, the
  list page gains its outputs, and a devices screen replaces the read-only
  targets screen. The 2026-08-21 brief and the current DESIGN.md describe the
  superseded unit; DESIGN.md remains the authority for the built surface until
  the rework lands and is rewritten with it.
- Because editing and building are separate calls, a caller that edits a list
  through the API without building leaves its outputs serving the previous
  composition. The store cannot currently detect that: a rebuild whose bytes
  are unchanged deliberately keeps the earliest content time (ADR 0004), so
  comparing the list's update time against the file's content time reports
  false staleness. Recording which list revision an output was built from is
  the fix and is not in this slice.
- A followed list has no single stored service set, so composition is resolved
  from the preset at read time. A preset that leaves the catalog must therefore
  keep the list working: the last resolved composition is retained and the list
  reports that its preset is gone, the same honesty rule a vanished target
  already follows.
- Auto v1, the planner, renderers, snapshot immutability and the loopback-only
  transport are unchanged. This ADR changes what the plan is built *for*, not
  how it is decided or rendered.

The composition, scheduling, naming and rebuild rules above were decided in the
same 2026-08-22 review round as the model itself and are recorded here rather
than in a second ADR.
