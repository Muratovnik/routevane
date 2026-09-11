# Architecture decisions

Audience: contributors investigating a decision, not users installing Routevane.
[Current architecture](../ARCHITECTURE.md), [requirements](../requirements.md), and
[UI behavior](../UI.md) describe what applies now. ADR bodies preserve the context
of the decision; an adopted ADR can have specifically replaced clauses.

Every ADR before 0039 is written in the vocabulary of its time, its title
included. Read such a body through this mapping rather than as current naming:
*service* is now a **list**; *list* or *route*, where it means the composition
published to a device, is now a **profile**; and *profile*, where it means the
renderer's version of an output such as `keenetic-bat-ipv4-v1`, is now a
**format**. An early ADR that already calls the composition a *profile* uses the
word the product has again.
[ADR 0039](./0039-one-product-vocabulary-across-binary-api-and-storage.md) made
the change and is the one place it is stated; earlier bodies are deliberately
not rewritten, because an ADR records the decision as it was taken.

Important chains: 0004 → 0013/0023 (objects and issuance), 0013 → 0016 → 0028 →
0029 → 0036 (composition, vocabulary, library ownership, default and route priority),
0028 → 0039 (the deferred identifiers, and the executable that carries them),
0027 → 0041 (one interface word per object),
0024 → 0025 → 0029 (editing),
0012 → 0014 → 0031 (credentials and unattended delivery), 0008 → 0032 → 0034
(Keenetic route ownership and descriptions), and 0002 → 0030 (Go).
Later clauses replace only the scope they name, not every invariant in an earlier ADR.

| Decision | Status |
| --- | --- |
| [ADR 000: use the supported Nuxt major](./000-use-supported-nuxt-major.md) | adopted |
| [ADR 0002: Use native Go toolchain gates with pinned analysis tools](./0002-use-native-go-toolchain-gates.md) | superseded |
| [ADR 0003: Keenetic IPv4 static-route BAT upload for Milestone 2](./0003-keenetic-ipv4-route-bat.md) | adopted |
| [ADR 0004: immutable local publication and opaque subscriptions](./0004-immutable-local-publication.md) | adopted |
| [ADR 0005: embedded generated UI under a self-only CSP](./0005-embedded-generated-ui.md) | adopted |
| [ADR 0006: official network feeds, destination policy, and source grace](./0006-official-feeds-and-source-grace.md) | adopted |
| [ADR 0007: browser discovery boundary and driver](./0007-browser-discovery-boundary.md) | adopted |
| [ADR 0008: device deployment boundary](./0008-device-deployment-boundary.md) | adopted |
| [ADR 0009: plugin protocol over stdio instead of gRPC](./0009-plugin-protocol-over-stdio.md) | adopted |
| [ADR 0010: domain-first observation and no network expansion](./0010-domain-first-and-no-network-expansion.md) | adopted |
| [ADR 0011: the routing plan / renderer boundary](./0011-routing-plan-renderer-boundary.md) | adopted |
| [ADR 0012: the device credential through the local API](./0012-device-credential-through-the-local-api.md) | superseded |
| [ADR 0013: the list is the product unit; outputs bind formats and devices](./0013-list-unit-and-outputs.md) | adopted |
| [ADR 0014: opt-in device credential storage in the operating system's store](./0014-opt-in-device-credential-storage.md) | superseded |
| [ADR 0015: public lists are material for our catalog, not the catalog itself](./0015-external-lists-as-catalog-material.md) | adopted |
| [ADR 0016: categories group lists; a list references, and the output deduplicates](./0016-categories-references-and-output-deduplication.md) | adopted |
| [ADR 0017: a second Keenetic target renders FQDN object groups](./0017-keenetic-dns-based-routing-target.md) | adopted |
| [ADR 0018: an archived list is frozen, not hidden](./0018-archived-lists-are-frozen-not-hidden.md) | adopted |
| [ADR 0019: an observed value becomes a route only if it is a destination](./0019-a-route-is-a-destination-not-an-observation.md) | adopted |
| [ADR 0020: a rollback outlives the request that triggered it](./0020-a-rollback-outlives-the-request-that-triggered-it.md) | adopted |
| [ADR 0021: Routevane owns the FQDN groups it wrote, and removes them](./0021-routevane-owns-the-groups-it-wrote.md) | adopted |
| [ADR 0022: Generate the changelog with git-cliff and publish the curated entry](./0022-git-cliff-changelog-generation.md) | adopted |
| [ADR 0023: subscriptions begin with a successful publication](./0023-subscriptions-begin-with-a-successful-publication.md) | adopted |
| [ADR 0024: an operator-defined service is a catalog entry, not a list note](./0024-operator-defined-services.md) | adopted |
| [ADR 0025: service truth is global, and a list is a page](./0025-service-truth-is-global-and-a-list-is-a-page.md) | adopted |
| [ADR 0026: destinations of three kinds, and a card that observes by itself](./0026-destinations-of-three-kinds-and-the-observing-card.md) | adopted |
| [ADR 0027: capacity joins composition, and «куда» is one section](./0027-capacity-aware-composition-and-one-connections-section.md) | adopted |
| [ADR 0028: lists live in categories, and a route is what publishes them](./0028-lists-live-in-categories-and-a-route-publishes-them.md) | adopted |
| [ADR 0029: composing is per route, and the library is its own flow](./0029-composing-is-per-route-and-the-library-is-its-own-flow.md) | adopted |
| [ADR 0030: Go 1.27 is the native toolchain baseline](./0030-go-1-27-native-toolchain-gates.md) | adopted |
| [ADR 0031: scheduled delivery is an explicit output-to-device binding](./0031-explicit-scheduled-device-delivery.md) | adopted |
| [ADR 0032: persist exact Keenetic static-route ownership](./0032-persist-exact-keenetic-static-route-ownership.md) | adopted |
| [ADR 0033: portable configuration transfers use fresh identities](./0033-portable-configuration-transfers.md) | adopted |
| [ADR 0034: Keenetic route descriptions come from plan provenance](./0034-keenetic-route-descriptions-from-plan-provenance.md) | adopted |
| [ADR 0035: Nuxt UI supplies interactive component mechanics](./0035-nuxt-ui-component-mechanics.md) | adopted |
| [ADR 0036: initialize route-local list priority from a library default](./0036-route-local-list-priority.md) | adopted |
| [ADR 0037: Electron desktop and an independently usable Go CLI](./0037-electron-desktop-and-independent-cli.md) | adopted |
| [ADR 0038: Windows application updates from GitHub releases](./0038-windows-application-updates.md) | adopted |
| [ADR 0039: one product vocabulary across the executable, API, and storage](./0039-one-product-vocabulary-across-binary-api-and-storage.md) | adopted |
| [ADR 0040: exact FQDN ownership belongs to a router and output](./0040-exact-fqdn-output-ownership.md) | adopted |
| [ADR 0041: one interface word per object — format and connection](./0041-one-interface-word-per-object.md) | adopted |
