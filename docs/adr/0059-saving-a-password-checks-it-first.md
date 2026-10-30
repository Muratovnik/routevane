---
status: adopted
---

# ADR 0059: saving a router password checks it with a read-only login first

This decision replaces two clauses of
[ADR 0050](0050-saved-connection-credential.md): that a password the device
refuses "is known only to a send, never to the registry, so it has no device
route code", and that the send page's opt-in saves only after a successful send
because nothing else contacts the router about the password. The rest of ADR
0050 is unchanged: saving stays an explicit act, the registry records the fact
and never the secret, and removal, revocation, automatic delivery and transfer
keep their rules.

## Context

Under ADR 0050, `POST /v1/devices/{id}/credential` stored whatever password it
was given. A mistyped password stayed in the operating system's store and was
found only by the next send, or by a scheduled delivery that failed while nobody
watched. Every Keenetic deployment already starts with a read-only probe that
signs in and asks the device what it is and whether the connection's interface
exists, and a preview of a DNS-based format runs the same probe. Since ADR 0058
a refused login is reported as its own cause, `device_auth_rejected`, so the
product can tell a wrong password from an unreachable router.

## Decision

- **A password is checked before it is kept.** Saving signs in to the device with
  the connection's stored address, account and interface and the offered
  password, using the deployer's own `Probe` after its connection check. The
  probe asks read-only questions and never changes the device: it is the step a
  preview and every deployment already run first. The check runs under the
  delivery gate, like a preview, so an address edit cannot slip between the check
  and the write.
- **Only a refused login stops the save.** The outcomes are:

  | Probe outcome | Stored | HTTP | Answer |
  | --- | --- | --- | --- |
  | The device refused the login | no | 422 | `{"error": "device_auth_rejected", "field": "credential"}` |
  | Login accepted, no interface of that name | yes | 200 | `"verification": {"result": "interface_missing", "code": "device_interface_missing"}` |
  | Unreachable, an unusable answer, a refused connection or another failure | yes | 200 | `"verification": {"result": "unverified", "code": "<stable code>"}` |
  | Login accepted | yes | 200 | `"verification": {"result": "verified"}` |
  | The target's deployer has no login to ask | yes | 200 | `"verification": {"result": "not_applicable"}` |

  A refused password replaces nothing: an earlier saved password stays saved. A
  missing interface means the password is right and the connection needs another
  interface, so the password is kept. An unreachable router says nothing about
  the password, and refusing to store it would make saving impossible while the
  router is off; the reply says the password is unchecked instead. A firmware
  version the format does not support still answered the login, so it counts as
  verified: compatibility is the send's question, not the password's.
- **The unverified codes are the deployment vocabulary.** `device_unreachable`,
  `device_answer_invalid`, `device_command_rejected` and `connection_invalid` mean
  what they mean on the deploy routes. A failure none of them describes is
  `check_failed`, because nothing was being deployed. A check that runs out of
  its own time is `device_unreachable`.
- **The check has its own budget.** It may take 15 seconds, one request's budget
  of the Keenetic deployer; a router that has not finished signing in by then is
  reported unreachable and the password is kept unchecked. Without that bound a
  slow router could spend the request's whole budget across the several requests
  of a login and leave no time to store the result. The credential route and the
  automatic-delivery route, which may save a password, get the screen's 20-second
  budget plus these 15 seconds. If the request itself ends during the check,
  nothing is stored, because nobody saw the answer.
- **The reply grows a sibling field.** A successful save answers
  `{"device": {…}, "verification": {"result": …, "code": …}}`; `code` is absent
  when the result is `verified` or `not_applicable`. Clients that read only
  `device` are unaffected.
- **Every path that saves a password checks it.** An enabling request to
  `/v1/devices/{id}/auto-delivery` that carries a `credential`, kept for clients
  written before ADR 0050, saves through the same step: a refused password
  answers the same 422 and enables nothing. The send page's opt-in saves through
  the credential route after a successful send, so it now signs in once more,
  read-only, with a password the send has just proved; the extra login is
  accepted rather than adding a request flag that skips the check, which any
  client could set. No other path saves a password: the command line takes one
  for a single deployment and never stores it, a configuration import carries
  none, and the scheduler only reads one.
- **The log names the outcome only.** The route records the result and its code.
  The password, the device's answer, its cookies and its address stay out of the
  log and out of every reply, as they do for a refused send.

## Consequences

A wrong password is caught where it is typed rather than at a later send or an
unattended delivery. Saving now contacts the router, so it takes as long as one
login and needs the router to be reachable from this computer to report
`verified`; a router that is off still gets its password saved, marked
unchecked.

A refused login is a failed attempt on the router like any other, so saving a
wrong password repeatedly meets whatever failed-login protection the router has,
as typing it into the router's web interface would.

The test build (ADR 0052) opens no device connection. A suite that saves a
password there gets `200`, the password stored and
`"verification": {"result": "unverified", "code": "device_unreachable"}`, at once.

A format added later whose deployer takes a password inherits the check through
its `Probe`. Its probe must stay read-only, which the deployment lifecycle already
requires of the step that runs before a backup exists.
