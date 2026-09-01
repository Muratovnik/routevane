---
status: adopted
---

# ADR 0023: subscriptions begin with a successful publication

## Context

ADR 0004 made a subscription token intentionally irrecoverable: only its ID and
hash are stored, and the complete URL is returned once. ADR 0013 later split a
list from its outputs and required the UI to add a format and publish it as one
operator action.

The implementation still created the token while adding the output. If the
following refresh, planning, rendering, validation, or database publication
failed, the output had no file but the only copy of its token had already been
returned inside a request the UI could not complete. Retrying could build the
file but could not recover the URL. The store also kept only successful
artifacts, so after a reload the interface could not distinguish “never tried”
from “the last attempt failed while an older file remains valid”.

## Decision

- Adding an output stores only its immutable identity. It creates no
  subscription and returns no bearer value.
- A successful build publishes the immutable snapshot and artifact first. The
  application then creates the output's one immutable subscription if it does
  not already exist and returns the complete URL only in that response.
  Subsequent successful builds do not rotate or repeat it.
- Every build attempt after the output is found records one immutable bounded
  result. Success points at the published artifact. Failure stores a stable
  reason code and the numeric rule-limit details when applicable; raw internal
  error text is not persisted or exposed as operator copy.
- A failed attempt never changes `latest_artifact_id` or
  `previous_artifact_id`. A multi-output rebuild continues with the remaining
  outputs and reports each output's persisted last attempt.
- Subscription creation remains separate from the artifact transaction. If a
  successful first build response is lost before the URL reaches the caller,
  the token is still deliberately non-recoverable; solving response delivery
  would require a different secret-exchange protocol, not plaintext recovery.

## Consequences

An output may honestly exist without an artifact or a subscription. Its first
failed build can be retried without having stranded a bearer credential, and a
later failed build remains visible while the last valid file and subscription
continue to work. List and output read models gain the last attempt but never a
token.

Schema migration 2 adds immutable `output_attempts`. The earlier development
profile schema identified by `PRAGMA user_version=3` is not treated as a newer
version of this lineage: it is structurally fingerprinted, backed up, imported
into lists/outputs, verified, and only then activated. Existing subscription
hashes and publication pointers survive that import unchanged.
