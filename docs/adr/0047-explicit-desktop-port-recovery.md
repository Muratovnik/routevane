---
status: adopted
---

# ADR 0047: Explicit recovery of an unavailable desktop port

## Context

The desktop persists its loopback port so subscription addresses survive a
restart (ADR 0037). Windows can later reserve a range that contains that port.
The saved address then cannot bind even when no other application is listening.
A generic startup failure offers no way to restore the desktop.

Changing the port also changes every previously copied subscription address.
The stored bearer hashes cannot reconstruct the original tokens, so recovery
must preserve subscriptions and explain how their existing addresses change.

## Decision

Keep the saved port on ordinary starts. A bind failure for an existing desktop
port produces one fixed startup protocol message without credentials or raw
diagnostics. Other startup failures retain their existing handling.

Offer an explicit choice in the native desktop dialog: quit without changing
state, or choose a new port. Explain before that choice that the operator must
replace the port in previously copied subscription addresses. The database,
subscription tokens, published artifacts and renderer preferences remain intact.

After consent, start the same Go executable with a one-shot recovery intent.
It still tries the saved address first. Only if that bind fails does it choose
a new loopback port (ADR 0048). Under the existing data lock, flush and replace
the saved port after the server is composed and immediately before announcing
readiness, so a composition failure keeps the previous port. Refuse
replacement if the saved value changed. If saving fails, close the candidate
listener and retain the previous port. Later ordinary starts reuse the
recovered port.

Never terminate or adopt a foreign listener, change Windows reservations, add
a second server, or change independent `serve` behavior. Use the existing Go
listener, same-directory file replacement and Electron native dialog; no new
dependency or platform service is needed.

## Consequences and verification

Recovery changes only the address's port. Existing subscription token paths
remain valid on the recovered address, but consumers using the old absolute
address must be updated by the operator. Automatic port rotation would hide
this change; altering Windows reservations would affect unrelated applications.
[ADR 0048](0048-desktop-port-band.md) chooses the recovered port outside the
ephemeral ranges where Windows places its reservations.

`cmd/routevane/desktop_test.go` covers ordinary refusal, explicit recovery with
the same subscription content and token, persistence on restart, preservation
of a foreign listener, and failed composition or file replacement with no
ready endpoint.
Filesystem tests protect expected-value replacement and corrupt-state refusal.
Desktop bridge tests distinguish the port failure from other startup failures
and forward recovery only when explicitly requested.
`web/tests/desktop/port-recovery.spec.ts` drives the packaged application
through the native question: quitting keeps the saved port, and choosing a new
port opens the interface, persists that port for the next start and leaves the
foreign listener running.
