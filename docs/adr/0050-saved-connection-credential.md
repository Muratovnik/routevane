---
status: adopted
---

# ADR 0050: a connection's saved password is its own fact

This decision replaces the clauses of
[ADR 0031](0031-explicit-scheduled-device-delivery.md) and
[ADR 0042](0042-identify-manual-deployment-before-device-effects.md) that tie a
stored password to automatic delivery: that disabling automatic delivery removes
the credential, and that a manual send may read a stored credential only while
automatic delivery is on. The rest of both ADRs, including the deployment
lifecycle, the delivery gate and the manual request contract, is unchanged.

Saving is amended by [ADR 0059](0059-saving-a-password-checks-it-first.md): a
password is checked with a read-only login before it is kept, a password the
device refuses is not kept and answers `device_auth_rejected` on the device
routes, and a successful save reports what the check found. The statements below
that a refused password is known only to a send, and that only a send proves a
password, no longer hold; everything else here is unchanged.

## Context

ADR 0014 stored a router password only as part of the consent to automatic
delivery, and ADR 0031 and ADR 0042 kept that coupling. An operator who sends to
the same router by hand therefore typed the password for every send, and the only
way to avoid it was to turn on unattended delivery they did not want. The two
decisions are different: keeping a password is a statement about where a secret
lives, and automatic delivery is a statement about whether Routevane may change
the device while the operator is away.

## Decision

- **Saving a password is an explicit act on a connection.** The operator enters
  it for that connection and it goes to the operating system's credential store,
  exactly as in ADR 0014: keyed by the connection's identity, never in SQLite,
  never returned by the API, never in a URL or a log. Saving again replaces the
  stored password. Nothing is saved implicitly, and a password supplied with one
  send is never saved by that send unless the operator asked for it on the send
  page; that opt-in saves only after the send has succeeded, because a preview of
  a router does not contact it and proves nothing about the password.
- **The registry records the fact, not the secret.** A connection has a boolean
  `credential_stored`. It is true only while the store holds a password the
  operator saved for this connection, and it is the only authority for reading
  one: a secret in the store that the registry does not record, such as one left
  by an interrupted write, is never used. A deployer that takes no credential
  never has the fact; it reports a stored password, not an intention.
- **Removing it is explicit too.** Removing the saved password deletes the secret
  first and then clears the fact, so a failed delete leaves the connection saying
  it still holds one. When the deployer needs a credential, removal also turns
  automatic delivery off in the same write, because that delivery cannot run
  without it.
- **Automatic delivery is a separate switch.** For a deployer that needs a
  credential it can be turned on only while a password is saved; otherwise it is
  refused with the stable code `credential_required`. A deployer that takes no
  credential needs only the consent, as in ADR 0031. Turning automatic delivery
  off keeps the saved password.
- **Revocation rules from ADR 0031 stay.** Changing the address, the account or
  the interface, and forgetting the connection, delete the stored password
  whether or not automatic delivery was on, clear the fact and turn automatic
  delivery off. A changed display name preserves everything.
- **A manual send uses the saved password.** When a send names a registered
  connection and carries no password, the saved one is used whether or not
  automatic delivery is on. A password in the request takes precedence for that
  attempt and is not stored. The ADR 0042 request contract, snapshot check and
  refusal codes are unchanged; a connection that needs a password and has none
  saved is refused with `connection_invalid`, as before.
- **Unattended delivery needs both.** The scheduler delivers only when the
  connection is opted in and, for a deployer that takes a credential, has a saved
  password. A device with only one of them is not sent to.
- **Transfer carries no passwords.** A configuration export never contains one,
  and an imported connection starts with `credential_stored` false and automatic
  delivery off.
- **A platform without a store cannot pretend.** Saving a password is unavailable
  where no credential store exists, as ADR 0014 required. Manual delivery with a
  password entered for the send, and automatic delivery to a credential-free
  deployer, remain available.

## API

- `POST /v1/devices/{id}/credential` with `{"credential": "<password>"}` saves or
  replaces the password and returns `{"device": {…}}`, which now carries
  `credential_stored`.
- `POST /v1/devices/{id}/credential/remove` with `{}` removes it and returns the
  device.
- `POST /v1/devices/{id}/auto-delivery` with `{"enabled": bool}` turns automatic
  delivery on or off. A `credential` in an enabling request is saved first, for
  clients written before this decision. Enabling without a saved password for a
  deployer that needs one answers `409` with `{"error": "credential_required"}`.

## Refusal codes

A refusal of a `/v1/devices/*` route carries a stable code in `error` and, when
the refusal is about one field, that field in `field`, so a screen can say it
beside the field instead of in a generic notice. A reply never repeats the
address, the account or the password it refuses.

| Code | Status | Field | Meaning |
| --- | --- | --- | --- |
| `address_rejected` | 422 | `address` | The address broke the deployer's destination policy or could not be read. Added by this decision. |
| `credential_invalid` | 422 | `credential` | The password offered for saving is empty or longer than the store keeps. Added by this decision. |
| `secret_store_unavailable` | 503 | none | The platform keeps no credential store, so a password cannot be saved. Added by this decision. |
| `connection_invalid` | 422 | none | Another part of the connection, such as a missing account, does not suit the deployer. Existing code, reused with its meaning from the deploy routes. |
| `credential_required` | 409 | none | Existing code, unchanged. |

`address_rejected` is narrower than `connection_invalid`, not a synonym: the
deploy routes answer a send whose address was refused with `address_rejected`
and `field`, with the same status 422 as the device routes and with
`attempt_started: false`, while the attempt journal keeps `connection_invalid`
for it. Another connection fault stays `connection_invalid` with status 400 on
the deploy routes. A password the device refuses is known only to a send,
never to the registry, so it has no device route code. The account and the interface are required by the client before a
request is made and carry no field code of their own.

The client checks only the shape of what it sends. Which networks a router may
live on is the server's policy, and the field shows the server's refusal.

## Migration

Schema version 16 adds `devices.credential_stored`. Before this version a password
was stored exactly for a connection that was opted in to automatic delivery and
whose deployer takes a credential. SQLite cannot ask a deployer, but registration
and import refuse any connection whose account does not match that requirement,
so a non-empty account is the stored form of the same fact. The migration sets
`credential_stored` for opted-in connections with an account and leaves every
other connection, including credential-free ones, at false.

## Consequences

A password now stays in the operating system's store while automatic delivery is
off. The accepted risk of ADR 0014 is unchanged in kind and now reaches an
operator who uses only manual sending; it applies because that operator chose to
save the password, and the interface states that at the moment they do.

The recorded fact can disagree with the store if the operator deletes the entry
in the operating system's credential manager. A send or a scheduled run then
fails with the existing credential-unavailable refusal rather than guessing, and
saving the password again or removing it restores agreement.

If a future proposal wants a password saved without the operator entering it for
that connection, or kept for a connection that was not registered, it replaces
this ADR rather than extending it.
