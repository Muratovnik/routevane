---
status: adopted
---

# ADR 0031: scheduled delivery is an explicit output-to-device binding

Replaces [`0014-opt-in-device-credential-storage.md`](0014-opt-in-device-credential-storage.md).

## Context

ADR 0013 says an output may name one device, and ADR 0014 permitted keeping a
device credential for scheduled delivery after explicit consent. The stored
model implemented the device consent flag but omitted the output-to-device
identity, so the scheduler could not deliver without guessing among every route
that published the same target. A guess would eventually send the wrong route
to a device.

ADR 0014 also treated operating-system credential-store availability as a
condition for every unattended delivery. That is true for an authenticated
router but false for the local sing-box deployer, which deliberately refuses a
username, password, and interface. Creating a dummy secret for it would weaken
the boundary and make the product unavailable on a platform where it needs no
secret at all.

## Decision

- An output stores zero or one registered device identity. Binding is allowed
  only when output and device have the same target. Deleting the device sets the
  binding to null and does not delete the output, its subscription, or any
  published artifact.
- Automatic delivery remains off per device until the operator explicitly
  enables it. A binding without that consent publishes normally and performs no
  deployment.
- A deployer that requires a credential may enable automatic delivery only when
  the operating system can store it. The secret is keyed by device identity,
  never stored in SQLite, returned by the API, placed in a URL, or logged.
- A deployer that requires no credential stores none. It may be enabled on a
  platform without a credential store, and the scheduled path resolves an empty
  credential as the correct input rather than as a missing secret.
- Disabling automatic delivery and forgetting a device remove any credential
  entry unconditionally, including an orphan left by an interrupted or failed
  flag update.
- Registration and updates pass their non-secret address, account, and
  interface through the target deployer's destination policy before SQLite may
  store them. A password is invalid at this boundary. Changing any of those
  three connection fields revokes automatic-delivery consent and removes the
  old credential; changing only the display name preserves consent.
- A scheduled run may deliver only the exact artifact successfully published by
  that same run. A failed build never causes the previous artifact to be sent as
  if it were new.
- Every scheduled attempt uses the existing deployment lifecycle unchanged:
  validate connection, probe, check compatibility, create and verify a backup,
  deploy, verify, and roll back after deploy or verification failure. Each
  device attempt has its own deadline; one failure is reason-coded and does not
  stop sibling outputs.
- The final binding, device, consent, and credential reads share one
  context-cancellable authorization gate with detach, rebind, edit, disable,
  and forget operations. Delivery holds the gate through deployment and any
  rollback, so no authorization change can interleave between the final check
  and the first device side effect.
- Manual delivery is unchanged: it still requires confirmation and carries a
  credential for one attempt only.

## Consequences

The scheduler becomes a narrow coordinator rather than a second deployment
implementation. Publication reports successful artifact identities,
`DeviceService` owns consent and credential access, and `DeploymentService`
continues to own device mutation. An installation copied to another account
still yields no router secret, while a credential-free local deployment does not
claim a security dependency it does not have.

The device registry stores all non-secret connection inputs, including the
device-side interface when its deployer requires one. The control surface must
let an operator bind or detach a compatible device on each output and must state
whether automatic delivery is enabled for that device.
