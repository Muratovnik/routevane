---
status: adopted
---

# ADR 0003: Keenetic IPv4 static-route BAT upload for Milestone 2

## Context

Milestone 2 needs one documented file mechanism that a user can apply manually
without Routevane connecting to a router. KeeneticOS 5.0.4 and later documents
BAT upload under **Routing → User-Defined Routes → Upload**. The upload dialog
lets the user select an existing interface. Keenetic's published device limits
state that a BAT file may contain at most 1024 lines.

This milestone does not generalize across other Keenetic import, CLI, or API
mechanisms. It also cannot establish hardware acceptance without a compatible
device and an operator-authorized maintenance exercise.

## Decision

- Add target `keenetic`, profile `keenetic-bat-ipv4-v1`, and renderer
  `keenetic-route-bat` for KeeneticOS 5.0.4 or later.
- Emit ASCII without a BOM, using CRLF and a final CRLF. Every line is exactly
  `route ADD <masked-ipv4> MASK <contiguous-dotted-mask> 0.0.0.0`.
- Support exact IPv4 addresses and non-default IPv4 prefixes only. Emit no
  comments, blank lines, domains, IPv6, default routes, deletes, metrics,
  interface names, or credentials.
- Keep the official 1024-line limit in the target profile. The 128 KiB byte
  bound is a Routevane local safety bound, not a claimed Keenetic limit.
- Sort by network address and prefix length. Deduplicate format-identical route
  lines because a repeated command adds no routing coverage and the independent
  validator rejects duplicates.
- Require the user to choose an existing VPN or WAN interface during import.
  Routevane does not probe that interface or encode it in the file.
- Write a validated candidate to a caller-selected directory inside the data
  root using a hash-controlled `.bat` name, same-directory exclusive temporary
  file, sync, and no-replace commit. This is not artifact publication and
  creates no latest, fallback, pointer, snapshot, or subscription state.

## Consequences

The file contains no device address or credentials and can be inspected before
use. Unsupported domain and IPv6 candidates remain explicit partial-coverage
diagnostics; a required component with no safe IPv4 rule or a global rule-limit
overflow fails before rendering and creates no file.

Import behavior is treated as additive and non-transactional because the
documentation does not establish rollback semantics for this workflow. Manual
use therefore requires a configuration backup, independent recovery access, a
maintenance window, and post-import route-count and routing verification.
Milestone 2 does not connect, apply, probe, verify, back up, or roll back a
router. Hardware acceptance remains **unverified**.

If maintained documentation or later hardware acceptance disproves the file
contract, disable this target and adopt a superseding ADR before changing the
dialect or selecting another mechanism.

## Official sources

- [Static routing and BAT upload](https://support.keenetic.com/titan/kn-1810/en/15880-static-routing.html)
- [KeeneticOS 5.0 release documentation](https://support.keenetic.com/buddy-6/kn-3411/en/55390-os-5-0.html)
- [Keenetic device functional limitations](https://support.keenetic.com/hero/kn-1012/en/49454-functional-limitations-of-devices.html)
