---
status: adopted
---

# Routevane architecture

Routevane is a single local process with one executable and one generated
static frontend. The initial dependency direction is:

```text
domain <- planner <- application <- infrastructure / sources / renderers / HTTP / UI
```

The domain does not import transport, persistence, discovery, renderer, device,
or UI packages. The planner produces a deterministic `RoutingPlan`; format and
device constraints are applied at distinct boundaries. `PlanSnapshot` and
`ArtifactBuild` are separately immutable records.

Only directories used by the current vertical slice are created. The adopted
implementation plan owns product details and acceptance. This file
owns the enduring dependency boundary.

Two decisions underlie everything below and are recorded on their own:
[`adr/0010-domain-first-and-no-network-expansion.md`](adr/0010-domain-first-and-no-network-expansion.md)
is why a name is the identity and one observed address never widens into a
network, and
[`adr/0011-routing-plan-renderer-boundary.md`](adr/0011-routing-plan-renderer-boundary.md)
is why policy lives in the planner and a renderer decides nothing. Every later
boundary in this document is a consequence of those two.

## Observation and build state

The single binary carries the database-free `preview` command and four stateful
operations: `refresh`, `build`, `doctor`, and `run`. A concrete YAML loader snapshots and
normalizes the complete built-in/local catalog before network or database work.
Each DNS source cycle atomically upserts typed resources, sightings, CNAME
relations, and a source-run record in SQLite WAL; manual seeds remain catalog
data and receive a source-run record without synthetic non-expiring sightings.

SQLite stores observation history, source revisions, lists and their resolved
composition, outputs, devices and service settings, immutable publication
history, subscription hashes, and immutable per-output attempt results.
Lifecycle is derived at a caller-supplied UTC cutoff: equality with
`valid_until` is stale and equality with the 90-day retention boundary is
archived. Planner reads use one coherent transaction and filter to catalog
source revisions. Policy acceptance remains in `RoutingPlan`, never in the
observation tables.

`build` runs the pure planner, renders and validates a bounded Raw JSON document
fully in memory, then writes an unpublished artifact through a private
same-directory temporary file, file sync, and atomic no-replace commit. There
are deliberately no snapshot, artifact-build, subscription, latest, fallback,
or publication records at that layer. `doctor` uses a non-mutating read-only database
inspection, and the fixed-delay scheduler alone owns the process advisory lock.

The Nuxt surface remains a smoke UI. HTTP/browser sources, real device
renderers, deployment, workers, registries, and plugins are not created ahead
of the code that consumes them.

## Target catalog and the first renderer

The service catalog may now contain an optional, separately revisioned
`catalog/targets/` directory. Target YAML is strict, bounded, normalized, and
link/reparse-safe; changing a target never changes the service catalog revision
used as the observation watermark. Refresh remains target-free and persists the
Raw JSON watermark only.

`build --target keenetic` reads every selected service at one cutoff and
composes one provenance-complete policy plan in the planner. The application
then applies the target's line limit once globally to the renderer's canonical,
deduplicated projection. Required coverage and limit failures stop before the
byte-slice renderer. The composition root hand-wires Raw JSON and Keenetic;
there is no renderer registry, manifest, descriptor, domain artifact, or device
adapter.

The Keenetic renderer emits and independently validates only the IPv4 static
route BAT dialect adopted in ADR 0003. A dedicated file boundary writes one
validated candidate under the data root with a hash-controlled name and
no-replace commit. It creates no publication, snapshot, latest, fallback, or
subscription semantics. Router probing, application, verification, backup, and
rollback belong to device deployment, not to rendering.

## Publication and subscriptions

`serve` is the single-binary local publication surface. It holds the same
long-lived data-root advisory lock as the scheduler and binds only IPv4
`127.0.0.1`. The HTTP transport rejects unexpected authority and non-loopback
peers, ignores proxy headers, requires an explicit same-origin JSON mutation
marker, and applies bounded request, header, timeout, and concurrency limits.
The root status document is server rendered and contains no script, secret, or
mutation control.

Schema version 1 is the adopted list/output baseline; forward migration 2 adds
`output_attempts`. A list owns composition and an output binds it to one target
format. `plan_snapshots` preserve exact canonical RoutingPlan JSON encoded
before renderer invocation; `artifact_builds` preserve validated payload
identity and content creation time. `output_attempts` records a bounded stable
failure code or the successful artifact without storing raw internal errors.
Database triggers make publications, attempts, subscriptions and output
identity immutable and verify that latest/previous pointers belong to the same
output.

Publication is file-first and transaction-second. The filesystem boundary uses
an anchored private data-root handle and commits content-addressed files under
`artifacts/published/<renderer>/`. One SQLite transaction inserts or verifies
the snapshot/build, advances `previous` and `latest`, and records the successful
attempt. A failed transaction can leave only an unpublished orphan. Any later
build failure records a separate failed attempt and never advances or removes
the previous valid pointer. Subscription reads verify the regular file,
expected path, size, and SHA-256 every time and may serve the independently
verified previous pointer without mutating state.

Opaque subscription tokens are bearer capabilities. Only their indexed token
ID and full-token SHA-256 are stored. Adding an output creates no token; after
the first successful publication, the application creates exactly one
immutable subscription and returns its URL once in that build response. Output
and status reads never repeat it. A failed initial build can therefore be
retried without having lost the only copy of a secret. This lifecycle is
recorded in
[`adr/0023-subscriptions-begin-with-a-successful-publication.md`](adr/0023-subscriptions-begin-with-a-successful-publication.md).

Pre-list development databases are handled as a separate import lineage rather
than as a numeric downgrade. On open, the store fingerprints the former
`PRAGMA user_version=3` profile schema by its columns and absence of `lists`,
checkpoints and verifies it, creates a canonical `VACUUM INTO` backup plus a
preserved raw source, imports each profile as one list and one output, and
preserves observations, snapshots, artifacts, pointers, subscriptions and a
success attempt for every latest artifact. The imported database is activated
only after schema and foreign-key verification; any failure leaves the source
and backup recoverable.

## The control surface

The same `serve` process serves the local UI and `/v1`; it does not start a
Node, Nitro, proxy, CORS, or separate web listener. Nuxt is client-only and its
complete generated public output is synchronized to an ignored package-local
directory immediately before the canonical Go build. A tracked non-hidden
marker keeps a clean checkout compilable without generated output. The embed
boundary computes a content digest over sorted file hashes and returns it in
`X-Routevane-UI-Digest`.

The static boundary serves root and deep SPA paths as no-store HTML and only
hashed `/_nuxt/` assets as immutable. Unknown API and asset paths remain 404.
The UI document and every static response use a strict self-only CSP: no inline
script/style, `unsafe-*`, wildcard, proxy, or external asset source is needed.
The embedding, generated-HTML rewrite, and CSP decision is recorded in
[`adr/0005-embedded-generated-ui.md`](adr/0005-embedded-generated-ui.md).
The HTTP authority, loopback peer, origin, content-type, and mutation-marker
guards are unchanged by the presence of the UI.

`catalog/builtin` contains product service definitions only, because the local
control surface offers that list verbatim. The diagnostic `example`
definition lives in `testdata/pipeline/catalog` and a test fails if it returns to the
product catalog.

The catalog API preserves the sorted service-ID list and adds a parallel safe
detail list of ID, title, and category. A build response is a bounded safe
projection: it omits raw RoutingPlan bytes and exposes only output, snapshot
metadata, artifact metadata, and server-derived publication summary. Snapshot
reads remain the explicit diagnostics boundary. The sole bearer exception is
the one-time `subscription_url` on the first successful build of an output;
list, output, catalog and diagnostic reads never expose it.

The UI follows pages → features → shared imports. It maintains an explicit
idle → creating → refreshing → building → ready/warning/failed state machine,
and retains a previously verified result visibly stale if a later attempt fails.
Artifact format is diagnostics-only: the default result names the target device
without renderer identity, version, or content type, and those fields render
only after the diagnostics disclosure is opened.

The subscription URL is volatile component memory only: it is masked until an
explicit reveal/copy action and is excluded from SSR payloads, URL/history,
storage, cookies, CacheStorage, service workers, DOM attributes, and diagnostics
before that action. The artifact download endpoint is token-free. Routing
evidence is lazy-loaded only on explicit diagnostics expansion.

DNS A/AAAA observations remain the atomic routing evidence boundary. CNAME is
supplemental provenance enrichment: an unavailable CNAME lookup does not discard
an otherwise valid address observation, while cancellation, invalid data, and
bounds failures remain atomic.

## Sources

A source type is bound to an implementation in exactly one composition registry,
so a catalog source type cannot be supported by one entry point and unsupported
by another. Each type keeps only its own configuration: a DNS entry cannot carry
a URL and a feed entry cannot carry DNS names. An external source entry carries
only the names supported by the plugin protocol plus the implementation revision
from the installed manifest. Composition refuses a missing external type or a
revision mismatch before a cycle starts. A source revision covers the decoder or
plugin implementation and the full configuration, so changing a feed URL,
format, plugin revision, or requested names invalidates the previous observations
instead of mixing two semantics under one identity.

The HTTP feed source accepts only an absolute credential-free HTTPS URL,
resolves the host itself, and refuses the connection when any returned address
is outside the public unicast policy. Redirect hops are bounded and revalidated
by the same URL and address checks. Response bytes, entry count, and time are
bounded; an oversized feed is refused rather than truncated. An entry that does
not normalize to a canonical address or prefix is counted as skipped and never
recorded. There is no proxy support, because a proxy would move the destination
decision outside the validated policy.

A prefix from the `official` source class routes with reason `official_rule`;
observed, community, and metadata prefixes stay quarantined, and explicit
trusted shared-network evidence still quarantines an official prefix. Source
health is read in the same transaction as the observations it explains. A source
whose latest cycle failed while its previous success is inside the grace window
is degraded: its expired observations stay routable with reason
`source_degraded` and an expiry equal to the grace window, and the plan carries a
`source_degraded:<service>:<source>` warning. Grace never revives an archived or
invalid observation. The decision is recorded in
[`adr/0006-official-feeds-and-source-grace.md`](adr/0006-official-feeds-and-source-grace.md).

## Formats

A renderer id resolves to an implementation through one registry; a target id
resolves to a device profile through the catalog. The two maps are separate
because several targets may share one format, and they are joined only by
`TargetProfile.RendererID`. Composition refuses a registry whose key, version,
or descriptor drifts from its implementation, and it refuses a target whose
renderer is missing, whose profile key does not match, or which claims a
capability the format cannot express. No request handler contains a per-format
branch.

`domain.RendererDescriptor` carries the format metadata a caller needs without
knowing the format: content type and bare file extension, plus the id and
version it must not drift from. Publication takes the artifact content type from
it, the download name takes the extension from it, and artifact storage takes
both the directory segment and the file suffix from it. The storage layer keeps
only a defence-in-depth byte ceiling; the per-format bound is the target
profile's `MaxArtifactSize`.

Seven built-in renderers exist: six end-user formats (Keenetic IPv4 routes,
Keenetic FQDN groups, sing-box source rule sets, OpenWrt dnsmasq nftsets,
MikroTik address lists, and AmneziaVPN split-tunnel sites) plus the diagnostic
Raw JSON plan. The sing-box format is a declarative match set carrying domains
and both address families, so the same profile reaches it with domain rules the
router dialect must drop and without observed addresses a domain-capable target
does not need. Each format validates its own bytes by decoding them independently
and rendering the decoded projection again, so a non-canonical document cannot
be published.

Serving an artifact requires the renderer that produced it to still be
registered at the same version; otherwise the bytes are refused rather than
served untyped. The steps for adding a format are in
[`adding-a-renderer.md`](adding-a-renderer.md).

A degraded refresh is not a failed refresh: when every failed cycle is inside its
grace window the observations remain usable, the build publishes them, and the
result names the degraded sources. When a grace window has closed the failure is
reported as a failure, and an explicit build still publishes what the healthy
sources support.

## Discovery

One user-supplied URL becomes one reviewable local service definition. The
registrable domain comes from the Public Suffix List, so a multi-label suffix is
handled by the same code path as a single-label one, and a bare suffix or an
address literal is refused for having no registrable domain.

A host reaches a draft because it sits under the entered site's registrable
domain and for no other reason. Every other host is a recorded candidate with a
reason code, which is what keeps a shared CDN, an authentication platform, and an
analytics endpoint out of the draft without a vendor list deciding activation.
A draft carries domains only: an observed address stays a candidate, and a seed
that parses as an address or has no registrable domain is refused at both the
draft and the catalog boundary.

The single managed page load runs in an isolated temporary profile that is
removed on success, failure, and cancellation. Every byte the browser sends
passes through an in-process CONNECT proxy started with the loopback bypass
removed, so nothing escapes the shared destination policy. Requests, distinct
hosts, bytes, and time are bounded, and a host counts as contacted only after
the policy accepted it. A browser never starts from an unconfirmed value: the
unconfirmed call returns exactly the URL the confirmed call will load.

The draft is written through the catalog's own strict decoder, committed by link,
and never replaces an existing definition. Observation runs after the write so it
uses the catalog's real revisions; a failed first cycle is reported and leaves the
reviewable service in place. The decision is recorded in
[`adr/0007-browser-discovery-boundary.md`](adr/0007-browser-discovery-boundary.md).

## Learning sessions

A learning session is a repeatable exploration: an ordered list of steps, each
naming the component every host observed during it is attributed to. The
scenario is a file, so the same exploration can be replayed and compared. An
imported HAR archive produces the same evidence shape from a page's component
label, so a live session and an import cannot diverge in meaning.

Activation is deterministic and structural. A same-site host attributed to a
required component that was actually exercised is accepted. Telemetry and
advertising are recorded and never activated. A component no step exercised
contributes nothing. Any host outside the registrable domain stays a dependency
and is never widened to a suffix, an ASN, or a network. Anything unattributed
stays a dependency rather than being guessed: there is no score, model, or
vendor list in the decision.

A learned definition carries one component per exercised area with its own DNS
source, and the routing seeds cover every component of the same service, because
the site's own domain is as true for its media area as for its core.

Session provenance is stored as `loaded_by`, `redirects_to`, and
`observed_in_session` relations under the `learning-session` source. That source
is not a catalog source, so its revision never matches an active one and session
provenance can never influence routing on its own. The human-readable evidence
document is written under the data root, not the catalog, so replaying a scenario
never changes a reviewed definition.

## Device deployment

Deployment is the one operation that reaches the local network. It goes through
`netpolicy.DeviceDestination`, a narrowly named exception that permits private
and link-local unicast only and still refuses loopback, multicast, carrier-grade
NAT, and the cloud metadata address. A device URL must be an address literal, and
redirects are refused.

The lifecycle order is the contract: probe, compatibility, backup, deploy,
verify, and rollback when deploy or verify fails. An unsupported firmware reports
an empty profile key so the refusal names the version found, and a backup must be
stored, read back, and hash-verified before a deployment starts. Verification
reads the device's own route table back and names what is missing and what is
unexpected.

Idempotence is structural: the deployer removes exactly the routes it previously
owned on the named interface and adds exactly the artifact's routes, so a route
on another interface is never touched and a repeated deployment issues no command
batch. The deployer validates the artifact with the renderer's own parser and
never renders or plans; rendering and deployment are separate packages.

The device password is read only from an environment variable, never a flag.
`Connection` carries it to the deployer; `DeviceInfo`, `BackupRef`, and the audit
record cannot. The audit trail records each step's identity, outcome, and duration
for failed attempts as well as successful ones. The decision and its verification
limits are recorded in
[`adr/0008-device-deployment-boundary.md`](adr/0008-device-deployment-boundary.md).

An output may store one nullable registered-device identity. The device target
must equal the output target; target-only matching is never used, because two
routes can publish the same format and a timer must not guess which one belongs
on a device. Deleting a device sets this binding to null while leaving the
immutable artifact and subscription history intact.

The scheduler first runs publication and carries forward only the exact
artifact IDs successfully published by that run. The automatic-delivery bridge
skips outputs without an explicit device and devices without opt-in consent,
reads a credential through `DeviceService` only when the deployer requires one,
and invokes `DeploymentService` with a fresh per-attempt deadline derived from
process shutdown rather than from publication or a sibling attempt. Its final
authorization reads and every binding, connection, or consent mutation share a
context-cancellable gate held through deployment and rollback. Publication owns
neither credentials nor transports; deployment owns neither timers nor device
consent. Each attempt keeps the lifecycle above, and a bounded failure code is
logged without stopping sibling outputs or removing the published file.

## External plugins

An external adapter is a separate program the operator installs. It reaches the
host only through the pipes its parent created — length-prefixed JSON frames on
its own standard input and output, versioned explicitly. There is no listener, no
port, and no socket, so nothing else on the machine can connect to a plugin or
impersonate the host. The reasoning, including why the plan's `versioned gRPC`
was not adopted, is in
[`adr/0009-plugin-protocol-over-stdio.md`](adr/0009-plugin-protocol-over-stdio.md).

`sdk/routevaneplugin` is the public contract and the only Routevane package a
plugin imports. `internal/plugin` is the host: `Discover` reads installed
manifests, `Load` checks the executable's SHA-256 for early diagnostics, and
`Start` checks it again while making the private snapshot it actually executes.
The handshake refuses any plugin whose reported name, version, kind, protocol
version, permissions, or format metadata differs from the manifest the operator
reviewed. Install-time facts — the executable name and its checksum — are
verified by the host and never reported by the plugin.

A plugin is given nothing of the host's: the child starts in its own directory
with a minimal environment and three streams, and no subscription token, device
credential, or database path is passed to it by environment or by frame. A
permission belongs to a kind, and `network` is a disclosure to the operator
rather than a capability the host hands over. Every value crosses back through
the host's own checks — a source's observations are re-parsed before they become
sightings, and a renderer's artifact must satisfy the plugin's own validator and
then the same publication path, size bound, and semantic hash as a built-in
format.

Bounds belong to the host: frames are capped, every call has a deadline, a hung
plugin is terminated, and a crashed plugin fails its own call and nothing else.
The private runner cannot execute plugin bytes until the parent installs its OS
containment. Windows uses a Job Object with process-tree, memory, and CPU limits;
Linux and macOS apply address-space, CPU, open-file, and core-dump limits before
`exec` and isolate the process group for termination. These are resource and
secret boundaries, not a claim that operator-installed code is a filesystem or
network sandbox. A plugin's standard error is read line by line and re-emitted
as the host's own structured records.

`cmd/routing-agent/plugins.go` is the one place adapters are resolved. Built-ins
are always present and a plugin that claims an existing renderer id or source
type is refused at startup, because the built-in is what the product's own tests
cover. `ROUTEVANE_PLUGINS_DIR` is the only place the host looks; unset, no plugin
host is started and the binary behaves exactly as it did before plugins existed.

## Built-in adapters

Each adapter is its own vertical slice. The count of formats is not a quality
metric, so an adapter is added only when it expresses something the existing
formats cannot.

The OpenWrt dnsmasq fragment (`internal/renderers/openwrtnftset`) is the first
dynamic set: the artifact carries no address at all. dnsmasq resolves each listed
domain on the device and adds the answers to an nftables set, so the routing
decision follows the service as its addresses change rather than freezing what
was observed at build time. The format therefore supports suffix matching and
nothing else — the underlying option matches every name under a domain, so an
exact rule would be silently widened and is refused instead. Its target profile
is frozen to that one capability shape, and the set and table identities are
fixed constants because a set in another table would not be matched by the
firewall rules the installation hint describes.

The MikroTik RouterOS script (`internal/renderers/mikrotik`) is the first
replacing artifact. Each section removes the entries this product owns before
adding the current ones, so re-importing a newer artifact converges instead of
accumulating — the opposite of the additive Keenetic import. RouterOS accepts a
DNS name in an address-list entry and maintains the resolved addresses itself, so
an exact domain is carried as a name and appears in both sections because each
list resolves its own family. A suffix is refused: an address-list entry is one
name, not a match pattern. The list names are fixed constants because the script
clears the list it writes, and a configurable name would let one artifact clear a
list the operator maintains by hand.

The AmneziaVPN split-tunnel list (`internal/renderers/amnezia`) exists for its
consumer rather than for a new capability: an operator running that client cannot
import a router script or a sing-box rule-set, and this is the document its own
importer reads. The schema is the one the client implements — a JSON array of
entries carrying a hostname and an address list. The client routes an entry whose
hostname is a prefix directly and resolves an entry whose hostname is a name when
the tunnel comes up, so the artifact leaves every address list empty rather than
freezing what this build observed. IPv6 and suffixes are refused because the
client would store such an entry and never route it.

The local sing-box deployer (`internal/deployers/singboxlocal`) applies a
published rule-set to a sing-box installation on this machine. It
is the deployer that reaches nothing: no address, no credential, and no network
call. Its destination is the file the operator's own configuration declares, and
the whole deployment is a bounded read, an atomic write, and a read-back.

That deployer forced one change to the deployment contract. What a connection must carry
is a property of the transport, not of deployment, so `Deployer` now declares
`ValidateConnection` and the application no longer requires a username and a
password of every deployment. The Keenetic transport requires an account, a
password, and an interface there; the local deployer refuses all three, because a
local deployment has nowhere to send a credential. Either refusal happens before
the probe, so nothing is touched.

Compatibility for a local installation is the configuration's own declaration
rather than a firmware version: a rule set of type local and format source is
what accepts the bytes this product publishes, and anything else reports an empty
profile key and stops the lifecycle. The backup is an envelope rather than raw
bytes, because a rollback must be able to restore the case where there was no
file at all. Verification proves the file on disk is the published artifact and
still satisfies the sing-box validator; it does not prove a running sing-box
reloaded it, which would need a control API this package deliberately does not
speak.

## Deployment from the control surface

The local screen applies what it publishes. `POST /v1/artifacts/{id}/deploy`
without `confirm` reports what would happen and contacts nothing; with `confirm`
it runs the same lifecycle the command line runs, because the API is a second
caller of one use case rather than a second implementation of it.

The device credential crosses this boundary and stops there: it is read from the
request body, handed to the deployer for that one call, and never stored,
logged, returned, or placed in a URL. The page clears its own field when the
attempt ends and keeps nothing in browser storage. The existing mutation guard —
a loopback client, a JSON content type, the request marker, and a matching
origin — is what authorizes the change. The reasoning and the accepted risk are
in
[`adr/0012-device-credential-through-the-local-api.md`](adr/0012-device-credential-through-the-local-api.md).

`Deployer.Requirements` is what makes the form honest. A transport declares
whether it authenticates and whether it attaches to a named interface, so a
destination on this machine never shows a password field and refuses one that is
supplied anyway. `GET /v1/deployments/targets` lists only the targets whose
format has a deployer, so an option that cannot work is never offered.

`DeploymentService` is composed next to `PublicationService`, not inside it, and
one adapter in `cmd/routing-agent/serve.go` presents both to the HTTP layer
without adding behavior. Publication owns formats; deployment owns transports.
