---
status: adopted
---

# Routevane architecture

Audience: contributors changing the running system. This is the current
architecture; historical alternatives are in the [decision index](adr/README.md).
The [requirements](requirements.md) define product guarantees and limits.

## Composition and dependency direction

`cmd/routevane` builds one executable. Its composition root wires application
services to SQLite, catalog files, source adapters, renderers, deployment, plugins,
and the loopback HTTP server. It also remains the standalone CLI distribution.
`desktop/` wraps the same binary with Electron; no second Go executable or
background service is installed. The shell owns a single window, tray and child
process. See [ADR 0037](adr/0037-electron-desktop-and-independent-cli.md).

Windows installation and application updates use electron-builder/NSIS and
electron-updater in the main process. The preload exposes only state and an apply
command validated against the main application frame. Backend shutdown precedes
file replacement. See [ADR 0038](adr/0038-windows-application-updates.md).

The desktop renderer uses the stable `routevane://app` origin. A main-process
protocol handler forwards only that authority to a loopback port. Go persists the
first OS-assigned port under its data lock so subscription URLs survive restarts.
It adds a per-launch credential received by Go through stdin. Renderer
Node integration is disabled, context isolation and sandboxing are enabled,
and external navigation requires a separate browser action. The existing HTTP
Host, mutation and CSP guards remain active. A tiny isolated preload exposes
only the serving origin for subscription URL validation, never credentials.

Closing the window keeps the tray and schedule alive. Quit closes the parent
pipe, cancels requests and permits bounded device recovery before terminating.
EOF also covers an unexpectedly terminated shell. Go's data lock continues to
reject concurrent writers, including an independently started CLI. Source
refresh, desktop update installation and CLI distribution have different owners;
the shell does not turn source scheduling into an Electron-only capability.

Domain values and pure planning do not depend on persistence, HTTP, browser or
device transports. Interfaces belong to their consumers. A renderer projects an
already decided plan; a target profile supplies format capabilities and limits;
a deployer changes one compatible destination. These are distinct responsibilities
([domain-first](adr/0010-domain-first-and-no-network-expansion.md),
[planner/renderer boundary](adr/0011-routing-plan-renderer-boundary.md)).

## Product objects and storage

The UI's **list** is an API `service`: a named destination set. A UI **profile**
is an API `list`: a stored composition of lists/categories and exclusions.
An output joins that profile to a target format and optionally a registered device.
It owns its subscription and published artifact chain. The terminology mapping
is deliberate compatibility debt, not two product models
([ADR 0028](adr/0028-lists-live-in-categories-and-a-route-publishes-them.md)).

Catalog YAML seeds service definitions, categories and targets. Operator changes
are stored as overlays in SQLite; removing a shipped item does not edit its file.
Library writes and profile selection are separate flows. A directly referenced
library object cannot be deleted; profiles can be archived and restored.
Published history is immutable
([ADR 0029](adr/0029-composing-is-per-route-and-the-library-is-its-own-flow.md)).

SQLite WAL stores observations, source revisions/health, composition, device
metadata, managed-route claims, settings, attempts, subscription hashes, and publication records.
Migrations validate the prior schema; the former profile schema has a dedicated,
backed-up import path. A data-root OS lock prevents competing serving/scheduler
processes. `doctor` inspects the database without migration or repair.

## Observation and planning

A bounded catalog snapshot precedes source work. DNS and HTTPS feeds record typed
observations under their exact source/configuration revision. Browser discovery
and learning add attributed evidence and reviewable definitions; they do not
turn unrelated hosts into active broad networks.

Validity is derived at the supplied cutoff. Policy is then decided per plan:
an observation can be usable for one target and unnecessary for a domain-capable
one. Canonical order and semantic hashes exclude incidental IDs, map order and
wall-clock metadata. DNS addresses never prove ASN/RDAP/network ownership.
Official source evidence is distinct from community observations. A failed source
inside its configured grace window can contribute explicitly degraded evidence;
invalid/archived data is not revived
([source grace](adr/0006-official-feeds-and-source-grace.md)).

`internal/discovery` uses chromedp and an isolated browser profile. Its bounded
CONNECT proxy validates every destination and redirect; Playwright supplies test browsers. Same-OS-user
processes are inside the stated local trust boundary
([discovery boundary](adr/0007-browser-discovery-boundary.md)).

## Rendering and immutable publication

One registry maps renderer IDs to implementations. Target profiles are a separate
catalog joined by renderer ID/version. Composition rejects inconsistent profiles
and collisions. Seven renderers exist: six user-facing output formats and Raw
JSON diagnostics; the [support matrix](usage.md#supported-outputs) owns the
user-facing acceptance claims.

Target projection and size/rule limits precede rendering. Each renderer validates
its own bytes independently. Publication writes a content-addressed file first,
then transactionally records the plan/artifact/attempt and advances latest/previous
pointers. A failed candidate never removes the previous artifact. Reads verify
path, regular-file identity, size and hash; a verified previous file may be served
without mutating publication state.

Adding an output issues no secret. Its first successful publication returns the
subscription URL once and stores only its token ID/hash. Subsequent reads cannot
reconstruct it. Subscription serving is loopback-only, not proof that a physical
router can fetch the URL
([issuance](adr/0023-subscriptions-begin-with-a-successful-publication.md)).

## HTTP and embedded UI

`serve` binds IPv4 `127.0.0.1` and owns the scheduler and data-root lock.
The HTTP boundary rejects unexpected authority/non-loopback peers, ignores proxy
headers, bounds requests, and requires same-origin JSON mutation markers.
There is no separate Node/Nitro server, CORS proxy, or remote listener.

Nuxt generates static client-only output. The build synchronizes it into an
ignored embed directory; a tracked marker keeps a clean source checkout
compilable before generation. HTML and assets use a self-only CSP. Deep SPA
paths serve no-store HTML; hashed assets are immutable; unknown APIs/assets
remain 404. `X-Routevane-UI-Digest` identifies the served assets. A plain Go
build can be API-only and reports that state
([embedding](adr/0005-embedded-generated-ui.md)).

The UI's layers are pages, widgets, features, entities, shared. Tokens, primitives,
interaction rules and page contracts belong to [UI.md](UI.md), not a second
architecture-side copy. Product state lives on the server; URL hashes carry page
location only; browser storage holds display preferences, never credentials.

## Deployment and scheduling

Deployment probes compatibility, stores and verifies a backup, applies exact
artifact bytes, reads back, and rolls back after a deploy/verify failure.
Recovery has its own bounded context, detached from request cancellation
([rollback](adr/0020-a-rollback-outlives-the-request-that-triggered-it.md)).
Device transports accept validated local-network destinations; local sing-box
delivery uses a declared file path and reaches no network.

Keenetic static-route ownership is persisted per output and exact
endpoint/target/interface scope. The application computes additions and
authorized removals; the deployer observes and applies them without depending on
SQLite. A profile is removable only after its last claim disappears and the ledger
says Routevane created it. Missing or retired ownership is additive and preserves
unknown same-interface profiles. The immutable plan also carries human category/list
labels; deployment compacts their union into the native description of an owned
profile and verifies it on read-back without changing BAT artifact bytes
([exact ownership](adr/0032-persist-exact-keenetic-static-route-ownership.md),
[descriptions](adr/0034-keenetic-route-descriptions-from-plan-provenance.md)).

Manual credentials arrive through the CLI environment or the local API request.
They are not persisted by the deployment call. Unattended credentials are stored
only through explicit consent and the OS secret-store boundary; unavailable
secret-store support cannot silently fall back to SQLite or browser storage.

The scheduler publishes due outputs independently and passes only successful,
exact artifact IDs to delivery. A device binding, connection metadata, consent
and credential lookup are revalidated under the common cancellable gate held
through deployment/recovery. A sibling failure does not consume another output's
deadline or remove its file
([scheduled delivery](adr/0031-explicit-scheduled-device-delivery.md)).

## External plugins

The host uses versioned length-prefixed JSON over private stdio pipes, not gRPC.
The SDK is the public package; plugins cannot import Routevane's internal packages.
The installed manifest is checked against the program's handshake. Executed bytes
come from a private checksum-verified snapshot, under platform resource limits.
No host credential/database path is supplied. Returned values cross the same
validation boundaries as built-ins.

Windows Job Objects constrain the process tree, memory and CPU; Unix runners use
rlimits and process-group cleanup. These are resource limits, not a filesystem or
network sandbox against deliberately installed malicious code.
An unset `ROUTEVANE_PLUGINS_DIR` starts no plugin host.
[Protocol decision](adr/0009-plugin-protocol-over-stdio.md),
[SDK](../sdk/routevaneplugin/README.md), and
[tested examples](../examples/plugins/README.md) own the details.
