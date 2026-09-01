---
status: adopted
---

# ADR 0020: a rollback outlives the request that triggered it

## Context

ADR 0008 fixed the device deployment lifecycle: probe, backup, bounded deploy,
verify, and automatic rollback, all in one vertical slice. The ordering exists
because skipping any step makes a failure unrecoverable — without a backup a
rollback is impossible, without a verify a partial write looks like success.

The lifecycle was implemented faithfully and then handed one context: the HTTP
request's. Every route shared a single 20-second budget sized for answering a
screen, and the rollback inherited it along with everything else.

Three things followed. A Keenetic deployment allows up to 15 seconds per RCI
call and makes several, so probe plus backup plus deploy plus verify could
exceed the budget on a slow router — after the device had been written. When the
budget expired, `Rollback` was called with the context that had just expired, so
it failed immediately and returned `ErrRollbackFailed` with the device left
half-configured. And an operator who closed the browser tab cancelled
`r.Context()`, producing the same outcome from an ordinary gesture.

A fourth constraint was invisible from the handler: `http.Server.WriteTimeout`
was 30 seconds for every route. Raising a route's context budget past it would
have had the connection cut mid-answer regardless of how long the context lived.

## Decision

- **The rollback runs on a context detached from the caller's, with its own
  budget.** The cancellation that failed a deployment must not also disarm the
  recovery from it. An expired request deadline and a closed screen are exactly
  the moments when the device is already changed and the backup is the only way
  back.

- **The rollback budget is counted from the failure, not from the start of the
  request.** It is deliberately generous. Putting a device back is the step that
  must not be the one to run out of time.

- **The route that changes a device gets a budget that fits its lifecycle.** The
  default request budget is sized for answering a screen; a deployment is not
  one. The budget is a property of the route, so it is declared with the route
  rather than applied to all of them.

- **A route with its own budget extends its own write deadline.** The server's
  write deadline is one value for every route, so a route whose budget exceeds it
  would be cut off underneath the handler. The deadline is extended for that
  response only, with a margin so the handler's context expires first and answers
  with its own error instead of losing the connection.

- **Route budgets live in the route table.** Which methods a route answers, how
  long it may take, and which handler owns it are one contract with three parts.
  They were three parallel switches over the same names, and they had already
  drifted: a path resolved to a route name no handler owned, and answered an
  empty `200`. One table makes that unwritable, and a test walks every path form
  against it in both directions.

## Consequences

- A deployment can now outlive the HTTP request that asked for it by the length
  of its rollback. The audit trail is still returned to the caller when the
  request survives; when it does not, the recovery still happens and is still
  recorded.

- `ErrRollbackFailed` now means the device refused the rollback, not that the
  host gave up on it. That is the only reading under which the error is
  actionable.

- The deployment lifecycle became testable without a device: cancelling the
  caller's context from inside the deploy step reproduces the exact failure this
  ADR is about, and the test asserts the rollback still ran with a deadline of
  its own.
