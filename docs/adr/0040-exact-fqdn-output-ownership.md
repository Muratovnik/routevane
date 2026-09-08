---
status: adopted
---

# ADR 0040: exact FQDN ownership belongs to a router and output

## Context

Keenetic FQDN names and routing attachments are device-wide. The prefix-based
reconciliation from ADR 0017 could remove another output's objects. Static
routes already use persisted creation evidence and commit ownership only after
read-back. That lifecycle fits FQDN groups; sharing static-route claims does not,
because a named group's interface and entries must remain one output's decision.
The existing SQLite store is sufficient; another registry or service adds no
capability required here. The vendor's [DNS-based route documentation](https://support.keenetic.com/explorer/kn-1613/en/51150-dns-based-routes.html)
defines automatic and exclusive route options separately. Neither a prefix nor
an automatic flag establishes creation ownership.

## Decision

Persist exact created names, entries and routing policy for each canonical
device endpoint and immutable output ID. Never infer ownership from the name,
interface or a matching desired object. An existing unowned desired name causes
a read-only refusal. Unknown legacy groups remain untouched; an operator must
review and migrate them manually before retrying a conflicting deployment.

New artifacts use `<prefix>-<output hash>-<list hash>-s<shard>` names. Prefix is
an optional per-output lowercase slug of at most 24 bytes, default `routevane`.
The output and list components are the first 6 and 8 SHA-256 bytes respectively;
explicit shard delimiters prevent a list named `alpha-2` colliding with alpha's
second shard. Every computed name is bounded to 64 bytes, and duplicate computed
names are refused. Hash truncation is a bounded naming compromise, not a claim
of mathematical uniqueness: an observed unowned name still blocks mutation.
Published artifact bytes remain immutable. Changing the prefix affects the next
build and removes only the old names recorded for that same output on that device.

Load ownership before backup and mutation; verify entries, interface, automatic
and non-exclusive policy before replacing the ledger in one SQLite transaction.
A save or ledger failure invokes bounded recovery. Recovery saves the restored
configuration and reads it back; only CRLF/LF differences are ignored. Unknown
differences are failures. There is no distributed transaction: interruption
before the ownership commit can leave unowned new objects, which a later run
must refuse rather than adopt. Failed recovery preserves the previous ledger
and reports that restoration could not be confirmed. Address reuse does not
identify a replacement physical router; retiring a stored connection revokes its
ownership rather than transferring deletion authority to a new device.

## Consequences

Outputs coexist without adopting each other's names. Prefix changes can clean up
known old names while retaining manual groups. Lost or retired ledgers require
manual migration; convenient prefix-based recovery would recreate the defect.
The ledger contains no credential. Full configuration recovery assumes no external
writer changes the router concurrently; physical firmware and interruption
acceptance remain unverified by local protocol doubles.
