---
status: adopted
---

# ADR 0018: an archived list is frozen, not hidden

## Context

ADR 0004 forbids deletion: a published artifact is immutable and a subscription
token is never rotated, so a list a device depends on cannot be taken away. The
list-outputs brief therefore named archival as the way a list leaves the shelf,
and described it in two clauses — the list "уходит из основного вида и
перестаёт обновляться", while "его файлы и подписка продолжают работать".

The second clause is unambiguous. The first is not. "Stops updating" plainly
covers the timer, but the brief did not say what happens when the operator
edits an archived list, gives it a schedule, binds a new format, or presses
rebuild. Reading it as *hidden* — a filter on the library view and a skip in
the scheduler — leaves every one of those writes working.

That reading does not hold up. A list whose composition can still be edited
while nothing rebuilds it accumulates a stored composition that disagrees with
the bytes its subscribers keep receiving, and no screen can honestly say which
of the two it is showing. Binding a new format is worse: an output with no
artifact is a subscription URL that resolves to nothing, created deliberately
on a list the product just said had stopped.

## Decision

- **Archival is a state of the list, stored as the moment it left the shelf.**
  `lists.archived_at_ns` is zero while the list is on the shelf. It is a moment
  and not a flag because "since when" is the question asked of an archived
  list, and because a flag beside a date is two facts that can disagree.
- **An archived list is frozen. The only write it accepts is restore.** Edit,
  schedule, bind an output, refresh and build are all refused. The refusal is
  one guard on the list itself, applied at the application boundary, so a route
  added later cannot forget it. Over HTTP it is `409`: the request is well
  formed and the state is what rejects it.
- **The build guard is on the output too.** A build addresses an output, and an
  archived list reached through one of its outputs would republish just as
  effectively as one reached through itself.
- **The scheduler skips an archived list before judging it due**, so the timer
  never records a failed refresh for an attempt it was never going to make.
- **Reads are untouched.** The list is fully readable, its outputs are listed,
  its published files download, and its subscription resolves to exactly the
  bytes it was archived with. This is the whole point of the state.
- **Archiving and restoring publish nothing.** Restoring returns the list as it
  left: what it publishes is what it published when it was archived, and
  changing that is the operator's next decision rather than this one's side
  effect.
- **Both verbs are idempotent.** Asking for the state a list is already in
  changes nothing and reports the list as it stands, including the original
  archival moment. The operator asked for a state, not for a transition.
- **The library keeps the row.** An archived list leaves the shelf, not the
  library: it sits behind one disclosure with its archival date and its file. A
  row that vanished would leave the operator with a working subscription they
  could not find, which is the failure ADR 0004 exists to prevent.

## Consequences

The freeze is a contract the surface can rely on rather than re-implement. The
list screen withdraws every control that would change the list instead of
disabling it, because a disabled control still promises the action lives there.

Because archival is a moment rather than a flag, and because Go's `encoding/json`
does not omit a zero `time.Time` under `omitempty`, every optional moment in the
publication API is tagged `omitzero`. Before this slice a list that had never
refreshed reported `last_refreshed_at` as the year one, which a client reads as
a real date; that is fixed here as a consequence rather than as a separate
change.

Archival is deliberately not a deletion path and does not become one later.
Nothing here frees storage, retires a token, or removes an artifact, and a
future request to reclaim space has to be argued against ADR 0004 on its own
terms rather than inheriting this state's permission.

What archival does *not* do is reconcile the device. A router that already
holds the groups this list wrote keeps holding them; removing them is the
device-state reconciliation ADR 0017 deferred, and archiving a list is not a
substitute for it.
