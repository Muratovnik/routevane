---
status: adopted
---

# Advanced usage

Audience: operators using the CLI, manual imports, or discovery. Start with the
[quick start](../README.md#download-and-start).

Run commands from an extracted release folder. `./routing-agent` resolves the
binary (use `./routing-agent.exe` explicitly on Windows if needed). Contributors
can first build using [CONTRIBUTING](../CONTRIBUTING.md) and substitute the
`.cache/build/` binary. Catalog and data paths are relative to the working directory.
The learning-scenario example is a [separate downloadable file](examples/learning-scenario.yaml),
not part of the runtime archive; save it and pass its actual local path.

## Supported outputs

These formats are built in. Rendering and validation are covered by repository
tests; that does not establish acceptance on a physical device or running client.

| Output | Use and acceptance limit |
| --- | --- |
| Keenetic IPv4 routes | BAT import or device delivery; protocol tests, no physical-router acceptance |
| Keenetic FQDN groups | KeeneticOS 5.0+ CLI import or delivery; protocol tests, no physical-router acceptance |
| sing-box source rule set | JSON file or atomic local-file delivery; live runtime reload is unverified |
| OpenWrt dnsmasq nftset | Manual installation; physical firewall integration is unverified |
| MikroTik address lists | Manual script import; device execution and interruption recovery are unverified |
| AmneziaVPN split-tunnel list | JSON import; client import acceptance is unverified |

Raw JSON is a diagnostic format. The sections below describe format prerequisites,
manual imports, and delivery behavior.

## CLI basics

Preview routing rules without publishing an artifact:

```powershell
./routing-agent preview --service example --target raw-json
```

The command performs a live, deadline-bounded DNS lookup. Its output is stable
for the same normalized observations and explicit cutoff; live DNS answers and
the wall-clock cutoff can naturally change between invocations.

Refresh, build, and inspect from the CLI:

```powershell
./routing-agent refresh --service example
./routing-agent build --target raw-json --service example
./routing-agent doctor
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
[`docs/adr/0006-official-feeds-and-source-grace.md`](adr/0006-official-feeds-and-source-grace.md).

`refresh` loads all direct `catalog/builtin/*.yaml` and
`catalog/local/*.yaml` files as one bounded snapshot before opening the database
or resolving DNS. `build` prints the absolute path of an unpublished artifact
under `data/artifacts/raw-json/<service>/` only after in-memory rendering,
validation, file sync, and an atomic no-replace commit. `doctor` is read-only:
it never creates, migrates, repairs, or checkpoints the database.

The in-process fixed-delay scheduler owns an OS advisory lock and runs one
refresh/build pair at a time:

```powershell
./routing-agent run --target raw-json --service example --interval 30m
```

Use `--catalog-dir` and `--data-dir` on `refresh`, `build`, `doctor`, and `run`
to select alternate roots. File and directory modes are restricted where POSIX permissions apply;
Windows `chmod` is not presented as an ACL isolation boundary.

## Manual Keenetic file build and import

Refresh every service whose current observations should enter the file, then
build the selected set. `--service` is repeatable and is sorted and deduplicated
before one all-or-nothing build:

```powershell
./routing-agent refresh --service youtube
./routing-agent refresh --service discord
./routing-agent build --target keenetic --service youtube --service discord --output ./data/artifacts
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

This manual file-build workflow does not connect to the router, probe its version or
interface, apply the file, verify traffic, create a backup, or roll back. Hardware
acceptance is **unverified**. The exact mechanism and official documentation are
recorded in
[`docs/adr/0003-keenetic-ipv4-route-bat.md`](adr/0003-keenetic-ipv4-route-bat.md).

## OpenWrt dnsmasq nftset fragment

The `openwrt` target builds a dnsmasq configuration fragment instead of a list
of addresses. The device resolves each listed domain itself and adds the answers
to an nftables set, so the routing decision follows the service as its addresses
change:

```powershell
./routing-agent refresh --service youtube
./routing-agent build --target openwrt --service youtube --output ./data/artifacts
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
./routing-agent build --target mikrotik --service youtube --output ./data/artifacts
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
./routing-agent build --target amnezia --service youtube --output ./data/artifacts
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

The release launcher starts the service for you. To run the binary directly
(the bind address is fixed to IPv4 loopback):

```powershell
./routing-agent serve --port 8765 --catalog-dir ./catalog --data-dir ./data --open-browser
```

The command prints its server-owned origin, normally `http://127.0.0.1:8765`.
The embedded UI opens after it is ready; omit `--open-browser` to open it manually.
Create a route, choose its lists and outputs, then refresh and build it.
The UI uses relative `/v1` requests. Mutating requests must use exact
`Content-Type: application/json` and `X-Routevane-Request: 1`; build never
performs an implicit network refresh.

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
in [`docs/adr/0005-embedded-generated-ui.md`](adr/0005-embedded-generated-ui.md).

`serve` and `run` are mutually exclusive long-lived data-root owners. The
product remains loopback-only and does not add remote authentication, channels,
or artifact garbage collection. The storage and security decision is recorded in
[`docs/adr/0004-immutable-local-publication.md`](adr/0004-immutable-local-publication.md),
and the first-success subscription lifecycle in
[`docs/adr/0023-subscriptions-begin-with-a-successful-publication.md`](adr/0023-subscriptions-begin-with-a-successful-publication.md).

The device selector lists every catalog target this build can serve. The same
service selection can be published for each of them: Keenetic receives the IPv4
route dialect, and `singbox` receives a sing-box source rule-set document that
carries the domain suffixes the router format has to drop. Each artifact is
validated by its own independent parser before publication, and the download
name and content type come from the renderer descriptor rather than a constant.
Adding a format is documented in
[`docs/adding-a-renderer.md`](adding-a-renderer.md).

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
./routing-agent discover --url shop.example.co.uk
```

The first call performs no browser load. It prints the canonical HTTPS URL, the
registrable domain the Public Suffix List derived, and the local service identity
it would create. Repeat the call with `--confirm` to run exactly that URL:

```powershell
./routing-agent discover --url shop.example.co.uk --confirm --service-id shop --title Shop --browser "C:/Program Files/Google/Chrome/Application/chrome.exe"
```

One managed page load runs in an isolated temporary profile that is removed on
success, failure, and cancellation. Every request passes through an in-process
proxy that refuses this machine and its local network, and requests, hosts,
bytes, and time are bounded. Hosts under the entered site's registrable domain
are configured for observation; every other host is reported as a candidate and
never activated. The draft is written to `catalog/local/<id>.yaml`, contains
domains only, and never replaces an existing definition. Observed addresses stay
in the local database. The boundary is recorded in
[`docs/adr/0007-browser-discovery-boundary.md`](adr/0007-browser-discovery-boundary.md).

Discovery needs a Chromium-family browser you already own; replace the example
Windows path with its actual executable on your OS. `--browser PATH` or
`ROUTEVANE_BROWSER` names it, and nothing is taken from the search path. Node.js
and Playwright are not runtime requirements.

## Learning a service from an exploration

One page load misses everything behind a sign-in or a play button. A learning
session replays a described exploration and attributes what it sees to the action
that reached it:

```powershell
./routing-agent learn --scenario learning-scenario.yaml
./routing-agent learn --scenario learning-scenario.yaml --confirm --browser "C:/Program Files/Google/Chrome/Application/chrome.exe"
```

A captured archive can be imported instead of driving a browser:

```powershell
./routing-agent learn --har session.har --url app.example.co.uk --confirm
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

The local screen can do this for you — see the [user guide](../README.md). Everything below is
the command-line equivalent, which reads the password from an environment
variable instead of a form.

Automatic deployment is opt-in and never needed: the manual download continues to
work unchanged. When you do want it, the device password comes from an
environment variable rather than a flag:

```powershell
./routing-agent deploy --artifact ID --target keenetic --device http://192.168.1.1 --user admin --interface Wireguard0
$env:ROUTEVANE_DEVICE_PASSWORD = Read-Host -AsSecureString | ConvertFrom-SecureString -AsPlainText
./routing-agent deploy --artifact ID --target keenetic --device http://192.168.1.1 --user admin --interface Wireguard0 --confirm
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
[`docs/adr/0031-explicit-scheduled-device-delivery.md`](adr/0031-explicit-scheduled-device-delivery.md).

The Keenetic RCI interface is documented by the vendor's community rather than by
a vendor specification, so this deployer is verified against a faithful device
double and **has not been accepted against a physical router in this
repository's environment**. It refuses what it cannot confirm rather than
guessing. The boundary and its verification limits are recorded in
[`docs/adr/0008-device-deployment-boundary.md`](adr/0008-device-deployment-boundary.md).

## External plugins

Use the [tested plugin examples](../examples/plugins/README.md) for installation
and the [SDK contract](../sdk/routevaneplugin/README.md) for development.

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
./routing-agent deploy --artifact ID --target singbox --device file:///C:/sing-box/config.json
./routing-agent deploy --artifact ID --target singbox --device file:///C:/sing-box/config.json --confirm
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
