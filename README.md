# Routevane

Routevane is a local service that maintains safe routing rules for internet
services and renders them for routers, proxy clients, and network tools. The
repository follows the vertical milestones in
[`docs/plans/routing-service-implementation-plan.md`](docs/plans/routing-service-implementation-plan.md).

The current tree implements Milestone 10. Every earlier milestone remains
available: the Milestone 1 observation pipeline and diagnostic Raw JSON build,
the Milestone 2 manual Keenetic build, the Milestone 3 publication service, and
the Milestone 4 local UI. A loopback-only server serves the Russian-language
control surface and the `/v1` API from the same process. It can create named
service lists with several target outputs, observe services through DNS and
protected official HTTPS feeds, publish immutable plan and artifact records in
more than one real format, and expose stable opaque subscription URLs with verified
previous-artifact fallback. A local service can also be derived from a single URL through one
managed browser session, or learned from a repeatable exploration or an imported
archive. Six built-in end-user formats serve routers, a proxy client, an OpenWrt device,
and a VPN client, and an already-published artifact can be applied to the
operator's own router or to a local sing-box. An external renderer or source the
operator installs is used through the same paths as a built-in one. The
Milestone 0 `spike` remains as a compatibility diagnostic.

## Download and run

A tagged release carries one archive per supported platform. Running one needs
no Go, Node, or Python. Windows can start it by double-click; Linux and macOS
use the included shell launcher.

| Host | Release archive | Acceptance in this repository |
| --- | --- | --- |
| Windows x64 | `routevane-<version>-windows-amd64.zip` | Native archive smoke test in CI; the full prerelease workflow is also exercised on Windows x64 |
| Windows ARM64 | `routevane-<version>-windows-arm64.zip` | Native archive smoke test in CI |
| Linux x64 | `routevane-<version>-linux-amd64.zip` | Native archive smoke test in CI |
| Linux ARM64 | `routevane-<version>-linux-arm64.zip` | Native archive smoke test in CI |
| macOS ARM64, macOS 13+ | `routevane-<version>-darwin-arm64.zip` | Native archive smoke test in CI |

There is no release archive for macOS Intel, 32-bit systems, containers, or a
system service manager. Those environments are not part of the release promise.
The browser suite uses Chromium; opening the UI in every system browser is not
claimed as separately accepted.

1. Download the archive for your platform and `SHA256SUMS` from the same release
   page. Verify that the archive hash is exactly the value on its line in
   `SHA256SUMS` (replace the example version and platform with the file you
   downloaded):

   ```powershell
   (Get-FileHash .\routevane-v0.1.0-windows-amd64.zip -Algorithm SHA256).Hash.ToLowerInvariant()
   ```

2. Unpack it. The archive contains `routing-agent`, the editable `catalog/`,
   `LICENSE`, `THIRD_PARTY_NOTICES.txt`, and `SBOM.spdx.json`.
3. Double-click **`start-routevane.cmd`** inside the unpacked directory. It
   starts the service and opens `http://127.0.0.1:8765` once it answers. On
   Linux and macOS run `./start-routevane.sh` instead. Pass a port to use one
   other than 8765.

   The window stays open while Routevane runs; close it or press Ctrl+C to
   stop. If you would rather run it yourself, the launcher does nothing more
   than `routing-agent serve`.

To build an equivalent archive set yourself (not a bit-for-bit reproducibility
promise): `pwsh -File tools/dev.ps1 release`.

## Output support matrix

“Built-in” means Routevane renders and validates the artifact in repository
tests. It does not mean a physical router or third-party client was exercised.
The distinction is deliberate: a successful build is not evidence that a
consumer loaded the result.

| Target | Built-in output | Application path | Downstream acceptance |
| --- | --- | --- | --- |
| Keenetic | IPv4 static-route BAT | Manual import and verified device delivery; KeeneticOS 5.0.4+ | Device protocol is covered by a faithful test double; physical-router acceptance is unverified |
| Keenetic (domain-based) | FQDN-group CLI commands | Manual import and verified device delivery; KeeneticOS 5.0+ | Device protocol is covered by a faithful test double; physical-router acceptance is unverified |
| sing-box | Source rule-set JSON | Manual configuration or atomic delivery to a local file | File shape, configured destination, backup and read-back are verified; live runtime reload is unverified |
| OpenWrt | dnsmasq nftset fragment | Manual install | Syntax is verified; physical-device and firewall integration acceptance are unverified |
| MikroTik RouterOS | Replacing address-list script | Manual import | Script shape is verified; physical-device execution and interruption recovery are unverified |
| AmneziaVPN | Split-tunnel site-list JSON | Manual import | Schema is verified; client import acceptance is unverified |

Raw JSON is a seventh built-in renderer used for diagnostics, not an end-user
target in `catalog/targets/`. External plugins add operator-installed formats or
source types; they do not change the acceptance status of these built-ins.

## Start here

From a fresh checkout of this repository, double-click
**`start-routevane.cmd`** in the repository root. It installs the
dependencies on the first run only, builds the binary with the control surface
inside it, starts the local service, and opens the page once it answers. Pass a
port as the first argument to use one other than 8765.

The same thing without the launcher:

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 setup
pwsh -NoLogo -NoProfile -File tools/dev.ps1 up
```

`up` takes `-Port 9000` for a different port and `-NoBrowser` to skip opening
the page. Stop either with Ctrl+C.

The control surface follows the objects the operator owns:

1. **Create a list.** Choose services directly or through catalog categories,
   then give the reusable selection a name.
2. **Add one or more outputs.** Each target comes from `catalog/targets/` and
   names its own format. Adding it refreshes, validates and publishes that
   output; other outputs continue independently if one format refuses the plan.
3. **Use the result.** The list page offers the current file and, after the
   first successful publication, the one-time subscription link. A failed
   attempt is persisted with its reason and never removes the previous file.
4. **Apply it.** For a destination this build can reach — a Keenetic router
   or a local sing-box — the screen shows a form asking only for what that
   destination needs, checks it, and then applies the file with a backup and
   an automatic rollback. The password is used once unless unattended delivery
   is explicitly enabled through the operating system's secret store.
5. **Register devices when useful.** `/devices` keeps concrete routers and
   applications separate from the read-only target catalog, and uses each
   deployer's declared connection fields.

The link is shown masked until you ask for it and is not recoverable after a
reload, so copy it when it appears. No IP address is shown in the normal path;
open «Расширенная диагностика» to see every rule, whether it was included or
excluded, and the reason code behind that decision. A partial coverage or a
degraded source appears as a warning rather than a silent difference, and a
failed rebuild keeps the previous validated result visible and marked as old.

The service listens on IPv4 loopback only. Product state and secrets never live
in browser storage; local storage holds display preferences only.

Everything below is for development, for the command line, and for applying an
artifact to a device.

## Stack

- Go 1.27.0 for the single `routing-agent` binary.
- Nuxt 4.5.2, Vue, and TypeScript for the local UI.
- npm with a committed lockfile.
- GitHub Actions plus the same local gate exposed through `tools/dev.ps1`.

The attached plan named Nuxt 3. Nuxt 3 reached end of support on 2026-07-31, so
the scaffold uses the maintained Nuxt 4 line. The decision and rollback are
recorded in [`docs/adr/000-use-supported-nuxt-major.md`](docs/adr/000-use-supported-nuxt-major.md).

## Prerequisites

For building from this repository:

- Go 1.27.0 or another supported 1.27 patch. Go 1.27 requires macOS 13 or
  newer when building or running on macOS.
- Node 24.19+ and npm 11.17+.
- PowerShell 7.4+ on Windows.
- Python 3.11+ for repository-only validators and hook installation.

## Development

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 setup
pwsh -NoLogo -NoProfile -File tools/dev.ps1 install-hooks
pwsh -NoLogo -NoProfile -File tools/dev.ps1 check
pwsh -NoLogo -NoProfile -File tools/dev.ps1 test-browser
```

A plain `go build` or `go run` produces a binary with no control surface: the
generated UI is written by `up`, `build`, `check`, and `test-browser`. Such a
binary still serves the API and says so at startup with a
`control_surface=absent` record naming the command to run.

`setup` installs build dependencies but does not modify the checkout's Git hook
directory. Contributors opt into the repository hooks with `install-hooks`.

`build`, `check`, and `test-browser` generate Nuxt's static public output and
safely synchronize it into the binary's ignored embedded-asset directory before
the Go product build. The generated output is never committed. Two consecutive
local builds are measured during delivery rather than assumed reproducible:
Nuxt's generated build metadata can carry a fresh build identifier. The build
command validates the generated target before replacement.

Run the technical routing spike:

```powershell
go run ./cmd/routing-agent preview --service example --target raw-json
```

The command performs a live, deadline-bounded DNS lookup. Its output is stable
for the same normalized observations and explicit cutoff; live DNS answers and
the wall-clock cutoff can naturally change between invocations.

Run the Milestone 1 pipeline:

```powershell
go run ./cmd/routing-agent refresh --service example
go run ./cmd/routing-agent build --target raw-json --service example
go run ./cmd/routing-agent doctor
```

A service may also declare an official network feed:

```yaml
sources:
  - id: official-feed
    type: http
    component: web
    config:
      url: https://feeds.example.com/ranges.json
      format: json
```

`format: text` reads one address or CIDR per line and ignores `#`/`;` comments.
`format: json` accepts a bare array of strings or an object whose `prefixes`
list holds strings or objects carrying `ip_prefix`, `ipv6_prefix`, `prefix`,
`cidr`, or `ip`. Only HTTPS is accepted; the destination address, every redirect
hop, the response size, the entry count, and the deadline are bounded, and an
entry that does not normalize is skipped rather than recorded. A prefix from a
published operator feed routes; an observed or inferred prefix stays
quarantined. A source that fails while its previous success is still inside the
grace window keeps its routes with a `source_degraded` warning instead of
dropping them. The policy is recorded in
[`docs/adr/0006-official-feeds-and-source-grace.md`](docs/adr/0006-official-feeds-and-source-grace.md).

`refresh` loads all direct `catalog/builtin/*.yaml` and
`catalog/local/*.yaml` files as one bounded snapshot before opening the database
or resolving DNS. `build` prints the absolute path of an unpublished artifact
under `data/artifacts/raw-json/<service>/` only after in-memory rendering,
validation, file sync, and an atomic no-replace commit. `doctor` is read-only:
it never creates, migrates, repairs, or checkpoints the database.

The in-process fixed-delay scheduler owns an OS advisory lock and runs one
refresh/build pair at a time:

```powershell
go run ./cmd/routing-agent run --target raw-json --service example --interval 30m
```

Use `--catalog-dir` and `--data-dir` on the M1 commands to select alternate
roots. File and directory modes are restricted where POSIX permissions apply;
Windows `chmod` is not presented as an ACL isolation boundary.

## Manual Keenetic file build and import

Refresh every service whose current observations should enter the file, then
build the selected set. `--service` is repeatable and is sorted and deduplicated
before one all-or-nothing build:

```powershell
go run ./cmd/routing-agent refresh --service youtube
go run ./cmd/routing-agent refresh --service discord
go run ./cmd/routing-agent build --target keenetic --service youtube --service discord --output ./data/artifacts
```

The build prints exactly one absolute `.bat` path after validation. An explicit
output directory must resolve inside `--data-dir`; omitting `--output` uses
`data/artifacts`. Unsupported domain or IPv6 candidates produce a structured
`partial_coverage` warning only when every required component still has a safe
IPv4 rule. Missing required coverage or more than 1024 unique projected BAT
route lines creates no file.

Before importing the file:

1. Confirm the router runs KeeneticOS 5.0.4 or later.
2. Make a router configuration backup and keep independent recovery access.
3. Schedule a maintenance window; the documented importer is treated as
   additive and its transactional/rollback behavior is unknown.
4. Inspect the BAT file and note its route-line count.
5. Open **Routing → User-Defined Routes → Upload**, select the file, and choose
   the intended existing VPN or WAN interface.
6. After import, verify the route count and test that representative service
   destinations use the intended route while recovery access still works.

Routevane Milestone 2 does not connect to the router, probe its version or
interface, apply the file, verify traffic, create a backup, or roll back. Hardware
acceptance is **unverified**. The exact mechanism and official documentation are
recorded in
[`docs/adr/0003-keenetic-ipv4-route-bat.md`](docs/adr/0003-keenetic-ipv4-route-bat.md).

## OpenWrt dnsmasq nftset fragment

The `openwrt` target builds a dnsmasq configuration fragment instead of a list
of addresses. The device resolves each listed domain itself and adds the answers
to an nftables set, so the routing decision follows the service as its addresses
change:

```powershell
go run ./cmd/routing-agent refresh --service youtube
go run ./cmd/routing-agent build --target openwrt --service youtube --output ./data/artifacts
```

The fragment carries suffix matches only. An exact domain is refused rather than
approximated, because the dnsmasq option matches every name under a domain.

On the router, once:

```sh
nft add set inet fw4 routevane4 '{ type ipv4_addr; flags interval; }'
nft add set inet fw4 routevane6 '{ type ipv6_addr; flags interval; }'
```

Add firewall rules that route members of those two sets through the intended
interface, then copy the fragment into `/etc/dnsmasq.d/` and restart dnsmasq.
Existing entries stay in a set until it is flushed, so flush both sets when a
service is removed from the profile.

Hardware acceptance on a physical OpenWrt device is **unverified** in this
repository; the option syntax follows the dnsmasq manual.

## MikroTik RouterOS address lists

The `mikrotik` target builds a RouterOS script that populates two firewall
address lists:

```powershell
go run ./cmd/routing-agent build --target mikrotik --service youtube --output ./data/artifacts
```

Unlike the Keenetic import, the script is replacing: each section removes the
entries Routevane owns before adding the current ones, so importing a newer
artifact converges rather than accumulating. RouterOS resolves a DNS name in an
address-list entry itself, so an exact domain is carried as a name; a suffix is
refused because an address-list entry is one name, not a match pattern.

On the router:

1. Upload the file and run `/import file-name=<file>`.
2. Match `routevane4` and `routevane6` in the routing or mangle rule that
   selects the intended interface.

The script clears only the two lists it owns. Do not rename them to a list the
router maintains by hand. Hardware acceptance on a physical RouterOS device is
**unverified** in this repository; the address-list syntax follows the RouterOS
documentation.

## AmneziaVPN split-tunnel site list

The `amnezia` target builds the site list the AmneziaVPN client imports:

```powershell
go run ./cmd/routing-agent build --target amnezia --service youtube --output ./data/artifacts
```

In the client, open split tunnelling, use the menu to import the file, and choose
whether it replaces the existing site list or is added to it.

Every entry carries an empty address list on purpose: the client routes a prefix
entry directly and resolves a name entry when the tunnel comes up, so the file
never freezes the addresses observed at build time. IPv6 and suffix rules are
refused because the client would store such an entry without routing it.

Acceptance against the client itself is **unverified** in this repository; the
schema follows the client's own importer.

## Local subscription service

The usual way to start the service is `tools/dev.ps1 up`, described in **Start
here**. To run it directly (the bind address is fixed to IPv4 loopback):

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 build
.\.cache\build\routing-agent.exe serve --port 8765 --catalog-dir ./catalog --data-dir ./data
```

The command prints its server-owned origin, normally `http://127.0.0.1:8765`.
Open it for the embedded local UI. It uses only relative `/v1` requests and one
action, **«Настроить автоматически»**, which creates a profile, refreshes it,
then builds a validated BAT artifact. Mutating requests must use exact
`Content-Type: application/json` and `X-Routevane-Request: 1`; build never
performs an implicit network refresh.

A direct clean `go run ./cmd/routing-agent serve` remains compilable through
the tracked UI marker, but does not contain generated UI assets. Use the
canonical `tools/dev.ps1 build`, `check`, or `test-browser` flow for an
UI-bearing product binary.

Adding an output does not create a subscription. Its first successful build
returns the subscription URL once; a failed initial build records its bounded
reason and can be retried without losing a secret. Treat the URL as a bearer
secret: Routevane stores only a token ID and hash and cannot display it again.
The UI keeps it only in component memory, masks it until explicit reveal or
copy, and intentionally loses it on reload; it never writes it to route state,
browser storage, cookies, caches, or diagnostics. Artifact download uses the
token-free artifact ID endpoint. The URL serves independently verified bytes with
strong SHA-256 ETag and immutable Last-Modified metadata. If the latest file is
missing or damaged, the independently verified previous artifact is served with
`X-Routevane-Fallback: previous`; if neither verifies, the service returns 503.

The root document and SPA deep links return no-store HTML. Hashed `/_nuxt/`
assets are immutable, unknown API and asset paths remain 404, and the UI carries
a strict self-only CSP. The response also includes `X-Routevane-UI-Digest` for
the embedded asset set. There is no Node/Nitro runtime, proxy, CORS layer, or
external UI asset at product runtime. The embedding and CSP decision is recorded
in [`docs/adr/0005-embedded-generated-ui.md`](docs/adr/0005-embedded-generated-ui.md).

`serve` and `run` are mutually exclusive long-lived data-root owners. The
product remains loopback-only and does not add remote authentication, channels,
or artifact garbage collection. The storage and security decision is recorded in
[`docs/adr/0004-immutable-local-publication.md`](docs/adr/0004-immutable-local-publication.md),
and the first-success subscription lifecycle in
[`docs/adr/0023-subscriptions-begin-with-a-successful-publication.md`](docs/adr/0023-subscriptions-begin-with-a-successful-publication.md).

The device selector lists every catalog target this build can serve. The same
service selection can be published for each of them: Keenetic receives the IPv4
route dialect, and `singbox` receives a sing-box source rule-set document that
carries the domain suffixes the router format has to drop. Each artifact is
validated by its own independent parser before publication, and the download
name and content type come from the renderer descriptor rather than a constant.
Adding a format is documented in
[`docs/adding-a-renderer.md`](docs/adding-a-renderer.md).

The default UI result shows operational status, selected services, artifact time,
target type, validation, and partial-coverage warnings. The artifact format is
not part of that view: renderer identity, renderer version, content type,
routing evidence, and excluded reasons appear only after opening
**«Расширенная диагностика»**.
Public/LAN access and separate workers remain outside this local release. Device
deployment, browser discovery, and the external renderer/source plugin path are
included and keep their own bounded safety boundaries.

## Adding a service by URL

```powershell
.\.cache\build\routing-agent.exe discover --url shop.example.co.uk
```

The first call performs no browser load. It prints the canonical HTTPS URL, the
registrable domain the Public Suffix List derived, and the local service identity
it would create. Repeat the call with `--confirm` to run exactly that URL:

```powershell
$env:ROUTEVANE_BROWSER = (node -e "console.log(require('playwright-core').chromium.executablePath())")
.\.cache\build\routing-agent.exe discover --url shop.example.co.uk --confirm --service-id shop --title Shop
```

One managed page load runs in an isolated temporary profile that is removed on
success, failure, and cancellation. Every request passes through an in-process
proxy that refuses this machine and its local network, and requests, hosts,
bytes, and time are bounded. Hosts under the entered site's registrable domain
are configured for observation; every other host is reported as a candidate and
never activated. The draft is written to `catalog/local/<id>.yaml`, contains
domains only, and never replaces an existing definition. Observed addresses stay
in the local database. The boundary is recorded in
[`docs/adr/0007-browser-discovery-boundary.md`](docs/adr/0007-browser-discovery-boundary.md).

Discovery needs a Chromium-family browser you already own; `--browser PATH` or
`ROUTEVANE_BROWSER` names it, and nothing is taken from the search path.

## Learning a service from an exploration

One page load misses everything behind a sign-in or a play button. A learning
session replays a described exploration and attributes what it sees to the action
that reached it:

```powershell
.\.cache\build\routing-agent.exe learn --scenario docs/examples/learning-scenario.yaml
.\.cache\build\routing-agent.exe learn --scenario docs/examples/learning-scenario.yaml --confirm
```

A captured archive can be imported instead of driving a browser:

```powershell
.\.cache\build\routing-agent.exe learn --har session.har --url app.example.co.uk --confirm
```

Both paths produce the same evidence and the same deterministic decisions. A
same-site host attributed to a required component that was exercised is
activated. Telemetry and advertising are recorded and never activated. A
component no step exercised contributes nothing, and every host outside the
registrable domain stays a dependency. The learned definition carries one
component per exercised area, session provenance is stored as `loaded_by`,
`redirects_to`, and `observed_in_session` relations that never influence routing,
and the readable evidence document is written under the data root.

## Applying an artifact to a device

The local screen can do this for you — see **Start here**. Everything below is
the command-line equivalent, which reads the password from an environment
variable instead of a form.

Automatic deployment is opt-in and never needed: the manual download continues to
work unchanged. When you do want it, the device password comes from an
environment variable rather than a flag:

```powershell
.\.cache\build\routing-agent.exe deploy --artifact ID --target keenetic --device http://192.168.1.1 --user admin --interface Wireguard0
$env:ROUTEVANE_DEVICE_PASSWORD = Read-Host -AsSecureString | ConvertFrom-SecureString -AsPlainText
.\.cache\build\routing-agent.exe deploy --artifact ID --target keenetic --device http://192.168.1.1 --user admin --interface Wireguard0 --confirm
```

The first call changes nothing. A confirmed deployment probes the firmware,
refuses an incompatible one before touching the device, stores and verifies a
configuration backup, installs exactly the artifact's routes on the named
interface, reads the device's route table back, and rolls back from that backup
if verification fails. A route on another interface is never touched and a
repeated deployment changes nothing. Every step is recorded with its outcome and
duration; no credential appears in the record.

For unattended delivery, register the device under **Connections** with every
non-secret connection field its deployer requests (Keenetic includes the route
interface), then turn on automatic delivery. A router password is stored only in
the operating system credential store; a local target such as sing-box needs no
password and creates no credential entry. On the route's **Connection** tab,
bind the matching output to that explicit device and set the route to daily or
weekly refresh. The scheduler then:

1. refreshes the route and rebuilds each output independently;
2. considers only an artifact successfully published by that same run;
3. delivers it only to the device explicitly bound to that output and opted in;
4. uses the same probe, compatibility check, verified backup, deploy, verify,
   and rollback lifecycle as a manual send.

A failed format is not delivered from an older file, a failed device does not
stop sibling outputs, and the previously published artifact remains available.
Disabling automatic delivery deletes the stored credential; forgetting the
device detaches it from outputs without deleting their files or subscriptions.
The consent, binding, and credential-free target rules are recorded in
[`docs/adr/0031-explicit-scheduled-device-delivery.md`](docs/adr/0031-explicit-scheduled-device-delivery.md).

The Keenetic RCI interface is documented by the vendor's community rather than by
a vendor specification, so this deployer is verified against a faithful device
double and **has not been accepted against a physical router in this
repository's environment**. It refuses what it cannot confirm rather than
guessing. The boundary and its verification limits are recorded in
[`docs/adr/0008-device-deployment-boundary.md`](docs/adr/0008-device-deployment-boundary.md).

## Installing an external plugin

An external renderer or source is a separate program you build and install. The
binary looks in one place only:

```powershell
$env:ROUTEVANE_PLUGINS_DIR = "$HOME\.routevane\plugins"
```

With that variable unset, no plugin host is started and everything below is
irrelevant — the built-in renderers and sources are complete on their own.

Each plugin lives in its own directory next to a manifest whose checksum matches
the executable:

```
plugins/
  my-renderer/
    my-renderer.exe
    manifest.json
```

The repository ships two working plugins to install as-is:

```powershell
go build -o "$env:ROUTEVANE_PLUGINS_DIR/csv-renderer/plugin.exe" ./examples/plugins/csv-renderer
(Get-FileHash "$env:ROUTEVANE_PLUGINS_DIR/csv-renderer/plugin.exe" -Algorithm SHA256).Hash.ToLower()
```

Put that digest in the directory's `manifest.json` as `sha256`, using
[docs/plugin-template/manifest.json](docs/plugin-template/manifest.json) as the
starting point. The executable is hashed before it is run, so replacing the file
without updating the manifest is refused rather than executed. Routevane checks
the bytes again while making the private snapshot it actually launches, closing
the gap between discovery and execution. It also enforces CPU, memory/address-
space, descriptor, and process-tree bounds appropriate to the host OS. These
limits contain faulty plugins; they do not turn deliberately installed code into
a filesystem or network sandbox.

A renderer plugin becomes selectable by adding a catalog target under
`catalog/targets/` whose `renderer` is the plugin's renderer id and whose
`profile_key` is its format version. A target that claims a capability the
plugin does not support is dropped rather than offered:

```powershell
go run ./cmd/routing-agent build --target examplecsv --service youtube --output ./out
```

A source plugin participates in the normal `refresh` cycle when a service names
its manifest type and exact implementation revision. Its `config.names` are the
bounded names sent in each observation request:

```yaml
sources:
  - id: static
    type: example-static
    revision: example-static-v1
    component: core
    config:
      names: [static.example.test, edge.example.test]
```

Use [docs/plugin-template/source-manifest.json](docs/plugin-template/source-manifest.json)
as the source-manifest starting point. A missing source plugin or a catalog/
manifest revision mismatch refuses the refresh instead of silently omitting the
declared source.

A plugin never replaces a built-in renderer or source; claiming an existing id
is refused at startup. To write your own, see
[sdk/routevaneplugin/README.md](sdk/routevaneplugin/README.md).

## Applying a rule set to a local sing-box

The `singbox` target can be applied to a sing-box installation on this machine.
The destination is the file your own configuration declares:

```json
{
  "route": {
    "rule_set": [
      { "tag": "routevane", "type": "local", "format": "source", "path": "routevane.json" }
    ]
  }
}
```

```powershell
go run ./cmd/routing-agent deploy --artifact ID --target singbox --device file:///C:/sing-box/config.json
go run ./cmd/routing-agent deploy --artifact ID --target singbox --device file:///C:/sing-box/config.json --confirm
```

A local deployment takes no account, no interface, and no credential; supplying
one is refused rather than ignored. The lifecycle is the same as for a device:
probe, backup, deploy, verify, and rollback on failure. The backup records the
previous rule set, including the case where there was none, so a rollback
restores exactly what was there.

A configuration that declares no local source rule set is refused before any file
is touched. Verification proves the file on disk is the published artifact and
still passes the sing-box validator; it does **not** prove a running sing-box
reloaded it. Restart or reload sing-box after a deployment.

## Repository map

| Path | Owner |
| --- | --- |
| `cmd/routing-agent/` | The single Go executable entry point |
| `internal/application/` | Refresh/build orchestration and consumer-owned seams |
| `internal/infrastructure/` | Strict catalog, SQLite, verified artifacts, loopback HTTP, and process lock |
| `internal/sources/`, `internal/renderers/` | Bounded DNS and HTTPS feed sources; six end-user formats plus diagnostic Raw JSON |
| `internal/discovery/`, `internal/netpolicy/` | URL-to-draft discovery and the shared destination policy |
| `internal/deployers/` | Device deployment boundaries, separate from renderers |
| `internal/plugin/`, `sdk/routevaneplugin/` | The plugin host and the public contract a plugin imports |
| `examples/plugins/` | A working external renderer and external source |
| `web/` | Nuxt UI and its tests; its empty `go.mod` isolates npm sources from root Go commands |
| `docs/` | Adopted plans, architecture, ADRs, and the renderer guide |
| `.agents/` | Project rules and canonical repo-scoped skills |
| `.claude/` | Minimal Claude discovery adapters for the two canonical project skills |
| `tools/`, `.githooks/` | Reproducible local gates and product hook delegates |
| `start-routevane.cmd`, `tools/launchers/` | The clickable entry points, for a checkout and for a release archive |

## Releases

A release is an annotated `vX.Y.Z` tag, and pushing that tag is what builds and
publishes the archives. Versions follow
[Semantic Versioning](https://semver.org/); while Routevane is `0.y.z` a
breaking change raises the minor and everything else raises the patch.

A version protects four surfaces: the local HTTP API, the catalog format, the
CLI flags, and the layout of the release archive. Package boundaries, the
database schema, and the generated UI internals are not part of it and may
change in any release.

Nothing writes the version down. `tools/dev.ps1 release` derives it from
`git describe` and stamps it into the binary, so the tag is the single source
and `routing-agent version` reports it back.

Cutting a release:

1. For the repository's first release, enable **Release immutability** in
   GitHub's repository settings. The workflow refuses to accept a published
   release that GitHub does not report as immutable.
2. Gates green: `pwsh -File tools/dev.ps1 check` and
   `pwsh -File tools/dev.ps1 test-browser`.
3. Render the entry, then curate it by hand — a short Highlights paragraph for
   a minor, and prose for anything breaking. Paste the result into
   `CHANGELOG.md` below `## [Unreleased]` and above the previous release:

   ```powershell
   npx --yes git-cliff@2.13.1 --unreleased --tag v0.1.0
   ```

4. Audit before tagging: `python .github/relkit.pyz audit --history --owner`.
   The tag is the trigger, so this must pass before the tag exists.
5. Commit as `chore(release): v0.1.0`, then create the annotated tag.
6. Push the tag. The release workflow binds the annotated stable SemVer tag to
   the event SHA, reruns the canonical, race, browser, history and commit gates,
   builds and structurally verifies all five archives, and executes each archive
   on a native GitHub runner. It then signs provenance and SBOM attestations,
   verifies them, uploads a draft, compares every uploaded digest, and publishes
   the complete immutable release.
7. Download the archive and verify both its checksum and signed provenance:

   ```powershell
   gh attestation verify .\routevane-v0.1.0-windows-amd64.zip --repo Muratovnik/routevane --signer-workflow Muratovnik/routevane/.github/workflows/release.yml
   ```

   Then start it and confirm `routing-agent version` reports the tag.

Steps 5 and 6 need a remote, which this repository does not have.

## Commits

Commits use Conventional Commits with the closed Angular type set:
`build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`,
`style`, and `test`. Install the repository hooks through
`tools/dev.ps1 install-hooks`.

The initial repository has no remote configured. Add and push a remote only as
an explicit publishing action.
