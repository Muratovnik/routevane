---
status: adopted
---

# ADR 0055: a deployment attempt names the saved connection it reached

The interface clause of this decision is amended: the Publishing tab now shows
the newest successful send of a format's file to any saved connection
(`latest_delivery`), naming that connection when it is not the bound one, and a
failed send that completed after it (`latest_failure`). `last_delivery` keeps
the meaning below for the clients that read it. The journal, its column and its
rules are unchanged.

## Context

The Publishing tab of a profile shows, for each format, when its file last
reached the connection the format is bound to. The server could not answer
that. The deployment journal, `deployment_attempts`, recorded the artifact, a
request hash, the outcome and its times, but not the connection: the deploy
command resolved a saved connection into an address and then cleared the
connection's id, and the request hash covers the address rather than the
connection. Scheduled automatic delivery did not write to the journal at all;
it called the deployment service directly.

Deriving the connection afterwards would be a guess. Two connections may share
an address, a connection's address may change, and a send to an address typed
by hand on the Send page names no connection at all.

## Decision

- **Schema version 20 adds `deployment_attempts.device_id`.** It holds the id of
  the saved connection the destination was resolved from, or the empty string
  for an address typed by hand. It is part of the attempt's immutable identity:
  the identity trigger now also refuses a change to it, and a retry of an
  attempt id with another connection is refused as a mismatch. Attempts written
  before version 20 keep the empty string, so no send made before the upgrade is
  reported; the column records what was known, it does not reconstruct it.
- **The output card reports the latest successful send to the connection the
  format is bound to now.** `last_delivery` carries the connection, the artifact
  and the completion time of the newest `succeeded` attempt among the output's
  artifacts whose `device_id` equals the output's current connection. A failed
  or pending attempt, a send to a typed address and a send to any other saved
  connection never count, however recent, so a one-off send elsewhere does not
  hide the bound connection's own delivery; an output bound to no connection
  has none. The interface still shows the date only while the reported
  connection is the one the format is bound to now.
- **Automatic delivery is journaled, and the journal never judges a send.**
  Scheduled delivery goes through the attempt journal with a fresh attempt id
  and the bound connection. If the journal cannot open an attempt, the send
  still runs, unrecorded, and the fault is logged: a journal outage must not
  stop delivery. If the journal cannot complete an attempt after a successful
  send, delivery is still reported as successful and the fault is logged; the
  row stays pending and is never counted. A send is therefore performed once and
  judged only by its own result.

## Consequences

- Rollback is forward-only, as with every schema version: an older binary
  refuses a version 20 database.
- A send whose journal entry could not be opened or completed has no date on
  the Publishing tab, although it reached the router. The log names it.
- The journal now has one row per automatic send as well as per manual one;
  rows remain immutable and are never deleted (ADR 0051 keeps the delivery log
  when a profile is deleted).
