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

## Manual HTTP request contract

`POST /v1/artifacts/{id}/deploy` accepts either raw `device`, `username`,
`password`, and `interface` fields, or one registered `device_id` with an exact
snapshot of the non-secret access shown to the operator:

```json
{
  "device_id": "dddddddddddddddddddddddddddddddd",
  "expected_connection": {
    "device": "http://192.168.1.1",
    "username": "admin",
    "interface": "Wireguard0"
  },
  "confirm": false
}
```

The registered form requires `expected_connection` and cannot mix in raw
top-level address, account, or interface fields. The server resolves canonical
metadata and checks the exact catalog target against the artifact's current
output target. Both preview and confirmed apply compare all three snapshot
fields before reading a saved credential or contacting the destination. A stale
address, account, or interface is refused with `connection_invalid`; changing
only the registration's display name is allowed. The request holds the same
delivery gate as metadata, consent, binding, and scheduled-delivery changes
through its final checks and device lifecycle.

A registered request may include a top-level `password` for this call. That
one-use value takes precedence and is neither saved nor substituted into the
registration. When a credential is required and the request omits it, only an
existing credential for that exact device with current automatic-delivery
consent may be read from the operating system's store. Missing consent or an
unavailable credential is refused. A credential-free deployer needs neither a
password nor automatic-delivery consent. Manual delivery does not enable
automatic delivery or change the output's device binding.

Apply adds `"confirm": true` and its `attempt_id` to the same selected snapshot.
The durable request hash uses the resolved non-secret address, account, and
interface. The registration ID and credential are not ownership authority, and
no credential is returned to the client. The existing raw-connection form
continues to work.

When the server proves that a confirmed request was refused before entering the
attempt service, it includes `"attempt_started": false` with the stable error.
This covers stale or deleted registrations, incompatible targets, unavailable
consent/credentials, and the request/attempt identity validations. It means this
request did not start another attempt; a reused ID may still name an earlier
attempt. The client may clear the refused request and plan again.

An empty returned attempt ID or an HTTP error alone is insufficient proof: a
journal failure can occur after its claim was persisted. Those failures, lost
responses, and pending outcomes keep the recovery behavior above. Clients treat
only the explicit boolean `false` marker as proof of a pre-attempt refusal;
an absent, malformed, or `true` value does not permit another write.

## Consequences

A transport error no longer claims that a device was unchanged. A completed
attempt is recoverable after a dropped response or server restart, while the
strict unknown state preserves the only truthful answer after a crash between a
device effect and final persistence. This journal is an operation lookup, not a
general deployment-history feature; the API exposes lookup by known identity
and no list endpoint.
