---
status: adopted
---

# ADR 0026: destinations of three kinds, and a card that observes by itself

## Context

ADR 0025 made the service card one table, but the table spoke only domains.
The catalog ships thin seeds — often a single domain — and the real material
lives in the automatic sources, which nothing read until the operator pressed
refresh; a freshly opened collection therefore looked almost empty. Observed
IP addresses and networks were filtered out of the table entirely, the manual
form rejected anything that was not a domain, and there was no way to bring in
an existing routes file. The card itself was a centered modal too narrow for
the table it carries, and the shell repeated the service address in a footer
line nobody needed. The owner named all of this as the flow still not being
thought through.

## Decision

- **A destination has three kinds.** Contents rows, verdicts and manual
  includes accept a domain, an IP address, or a network prefix. One canonical
  form per kind (lowercased domain, `netip` canonical address, masked prefix)
  is the identity everywhere: the verdict table, the registry, the seeds an
  include plants (`domain_suffix`, `ipv4`/`ipv6`, `prefix4`/`prefix6`). The
  planner already spoke all six seed kinds; the tuning layer stops narrowing
  them.
- **The card shows everything the next build reads.** Contents rows carry
  `value` and `kind` and merge seeds and stored sightings of every kind —
  domains first, then addresses, then networks. Excluding works uniformly: a
  catalog address seed is switched off the same way a fed domain is.
- **Verdicts are written in batches.** One action — a paste, a file — is one
  bounded request (`values[]`, at most 1024 per batch, 2048 standing verdicts
  per service, atomically rejected when any value is invalid). The former
  64-domain bound was sized for typing, not importing.
- **A file import is a parse, not a new storage kind.** The browser parses a
  plain list, a JSON array, or a Windows routes file (`route ADD <ip> MASK
  <mask>` becomes a prefix) and submits the result through the same verdict
  batch. Nothing new is stored, nothing re-read later: an imported file is the
  operator's standing word, exactly like a typed entry.
- **The card observes by itself.** Opening a service whose sources were never
  read triggers one automatic refresh and says so, so the first look at a
  collection shows its real contents instead of one catalog seed. The manual
  refresh stays for re-reading.
- **The card is a full-height side sheet.** Still one modal `<dialog>` with
  the page scroll locked behind it, but pinned to the right edge at up to
  64rem wide — sized for a table, not for a message.
- **The shell drops the runtime footer.** The service address is a Settings
  fact; the chrome does not repeat it.

## Consequences

The stored verdict table keeps its shape (`service_domain_verdicts`), with the
`domain` column now holding any canonical destination value — no migration,
because canonical domains, addresses and prefixes cannot collide. The registry
read bound for verdicts grows so a per-service cap of 2048 cannot be silently
truncated globally. Auto-observe performs network reads on card open; that is
operator-initiated (the open), bounded by the same refresh budget, and never
scheduled. Rendering hundreds of observed rows is accepted as the honest cost
of showing the material a build actually uses.
