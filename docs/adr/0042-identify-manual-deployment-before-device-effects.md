---
status: adopted
---

# ADR 0042: identify a manual deployment before device effects

## Context

A confirmed manual deployment was represented only by its HTTP request. If the
device changed and the response was lost, the control surface reported a
transport failure as though the change had not happened. Repeating that request
could run the whole device lifecycle again. The existing immutable artifact and
step audit described an answer once received, but supplied no durable operation
identity with which to recover a missing answer.

The deployment lifecycle in ADR 0020 already keeps rollback alive after request
cancellation. Outcome persistence must not be disarmed by the same cancellation
after rollback finishes.

## Decision

- A confirmed manual deployment carries a client-generated 128-bit lowercase
  hexadecimal attempt ID. Planning remains read-only and needs no attempt ID.
- Before the first device effect, Routevane durably claims the ID for the exact
  artifact and a SHA-256 binding of its non-secret destination fields. The
  password is excluded from the binding and is never written to SQLite.
- Reusing an ID for the same request returns its recorded outcome without
  replaying device effects. Reusing it for a different artifact, address,
  account, or interface is rejected.
- The final result and stable failure code are written after the existing
  deployment and any rollback, using a cancellation-independent five-second
  persistence budget. Schema version 15 adds the immutable
  `deployment_attempts` journal.
- `GET /v1/deployment-attempts/{id}` returns the terminal audit when known. An
  attempt left pending by process death is reported as `outcome_unknown`; it is
  never converted to failure and never resumed automatically.
- The control surface retains the current attempt ID in memory and, when
  available, in tab session storage. A lost response or reload checks that ID,
  blocks another apply action while the result is unknown, and offers an
  explicit status check. It never persists the credential.
- Confirmed requests from older HTTP clients without an attempt ID are rejected
  before deployment with `attempt_id_required`. The CLI deploy command calls the
  application lifecycle directly and remains unchanged.

## Consequences

A transport error no longer claims that a device was unchanged. A completed
attempt is recoverable after a dropped response or server restart, while the
strict unknown state preserves the only truthful answer after a crash between a
device effect and final persistence. This journal is an operation lookup, not a
general deployment-history feature; the API exposes lookup by known identity
and no list endpoint.
