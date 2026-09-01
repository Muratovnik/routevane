---
status: adopted
---

# ADR 0025: service truth is global, and a list is a page

## Context

The surface mirrored its storage instead of the operator's mental model. A
service showed two lists — the catalog's permanent domains and, separately,
whatever the sources returned — because that is how the planner stores them. An
operator could not switch a shipped source off, could not add a feed of their
own, and could only override a shipped service's domains per list (ADR 0024's
dialog split). Navigation was similarly split: the composer was a page, an
existing list opened in a drawer over the shelf, and the drawer neither locked
the page scroll nor gave the picker room. The owner rejected the whole shape:
"списки разделены… нельзя включать/отключать источники и добавлять свои…
всё какое-то скомканное".

## Decision

- **A list is a page.** `/lists/{id}` renders the same editor the composer
  uses; the drawer and its focus-trap plumbing are removed. Tab and first-setup
  state travel in the URL hash (`#tab=…&setup=…`) because the embedded server
  rejects query strings; legacy `/#list=` URLs redirect.
- **The service card is one table.** `GET /v1/services/{id}/contents` merges
  the catalog seeds, operator additions, and the stored planning snapshot into
  rows, each naming its origin (catalog, added by you, source id) and carrying
  one switch. A switch writes a **global per-service verdict**
  (`service_domain_verdicts`, include | exclude; absence means auto), so a
  change reaches every list on its next rebuild. Verdicts are service policy;
  sighting validity stays untouched, preserving the observation/policy split.
- **Sources are managed objects of the service.** A shipped source can be
  disabled per service (`service_disabled_sources`); an operator can attach
  their own HTTPS feed (`custom_sources`, generated `feed-` identity, formats
  `text`, `domain-list`, `json`, at most 8 per service and 64 total). Feed
  addresses pass the same public-unicast HTTPS validation as catalog feeds,
  injected as configuration so the application layer stays free of
  infrastructure imports. Tuning lives in schema v5 and hydrates a
  write-through registry at startup, like custom services (ADR 0024).
- **The planner keeps one catalog revision per plan.** Tuned definitions drop
  disabled sources, append custom feeds under a constant revision, and replace
  excluded seeds; per-service revision maps come from the tuned definition.
- **English is the primary language.** The locale fallback, the document
  default, and the e2e walkthroughs are English; Russian remains a complete
  dictionary proven by a dedicated localization test.

## Consequences

ADR 0024's consequence "a shipped service edits its list-local override" is
superseded: the card edits the service itself, and the per-list
`service_domains` override remains only as stored composition data. A service
refresh is exposed per service (`POST /v1/services/{id}/refresh`) with its own
one-minute budget. Removing a custom feed also removes its disabled markers;
shipped sources can only be disabled, never deleted. Published artifacts stay
immutable: every tuning change reaches subscribers only through the next
refresh-and-rebuild.
