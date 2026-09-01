---
status: superseded
---

# ADR 0014: opt-in device credential storage in the operating system's store

Superseded by [`0031-explicit-scheduled-device-delivery.md`](0031-explicit-scheduled-device-delivery.md).

Replaces [`0012-device-credential-through-the-local-api.md`](0012-device-credential-through-the-local-api.md).

## Context

ADR 0012 let the browser screen apply an artifact to a router and refused to
keep the password: it travelled once in a request body and stopped at the
deployer. That ADR named the consequence plainly — no "remember this device",
no unattended or scheduled deployment — and required a replacement rather than
an extension if that ever changed.

ADR 0013 changes it. A list can now refresh and rebuild on a schedule, and an
output that names a device can carry the result to the router. Delivery without
the operator present cannot ask for a password, so either unattended delivery
does not exist, or the credential outlives one attempt. The owner chose the
second, bounded by explicit per-device consent.

Storing the secret in SQLite beside the data would mean a copied database file
is a copied router login. An encryption key in a sibling file moves the problem
one file across. A master password at start-up is the strongest option and the
one that defeats the purpose: after a reboot nothing is delivered until a human
types it, which is the situation unattended delivery exists to remove.

## Decision

Automatic delivery is off for every device, and turning it on is what permits
storage. Nothing is stored implicitly.

- **The operating system holds the secret.** The credential goes to the
  platform secret store — DPAPI on Windows, Keychain on macOS, Secret Service
  on Linux — keyed by device identity. SQLite stores the reference, never the
  secret and never a reversible form of it. A database file copied to another
  machine or another account does not yield a router login.
- **Consent is explicit and specific.** Enabling automatic delivery for a
  device states, in the interface, that the credential will be kept in the
  operating system's store until automatic delivery is turned off. A toggle
  reports the applied fact, so it does not switch on before the store accepts
  the secret.
- **Turning it off deletes it.** Disabling automatic delivery, deleting the
  device, or a device whose deployer stops requiring authentication removes the
  stored entry. Removal failure surfaces as an error rather than a silent
  success, because a credential believed deleted is worse than one known kept.
- **Reading is narrow.** The stored credential is readable only by the
  scheduled delivery path for that one device. It is never returned by any API
  response, never rendered into a page, never logged, and never placed in a
  URL — the ADR 0012 rules on logging and response shape carry over unchanged,
  and the tests that assert them stay.
- **Manual delivery is unchanged.** With automatic delivery off, the password
  is supplied per attempt, travels once over loopback, is dropped after the
  attempt, and the page clears its field. The two-step plan-then-confirm stop,
  the mutation guard (loopback client, JSON content type, request marker,
  same-origin), and `Deployer.Requirements` driving the form all carry over
  from ADR 0012 verbatim.
- **A platform without a store cannot pretend.** Where no secret store is
  available, automatic delivery is unavailable and says so. There is no
  fallback to a file, and no "encrypted" placeholder that is really obfuscation.
- **Unattended delivery keeps the safety lifecycle.** Probe, compatibility,
  backup, deploy, verify, and rollback on failure run exactly as in an
  interactive attempt, and a scheduled attempt records the same audit trail. A
  failed unattended delivery never removes the artifact already published and
  is reported on the device and in the list's history.

## Consequences

The accepted risk grows in one specific way over ADR 0012: an attacker who
already controls the operator's logged-in account can ask the operating system
to release the credential, because that is what the store is for. Machine
theft, database copying, backup exfiltration, and any other account remain
covered. This is the ordinary bargain of every password manager and is stated
to the operator at the moment they opt in.

Three implementations of one narrow interface are now required, and the
integration is real per platform rather than a shared library call. A platform
that gains no implementation loses the feature, not the product.

If a future proposal wants storage without explicit consent, storage of a
credential the operator never entered, or delivery to a device the operator did
not register, it replaces this ADR rather than extending it.
