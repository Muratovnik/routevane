---
status: adopted
---

# Advanced usage

Audience: operators using the CLI, manual imports, or discovery. Start with the
[quick start](../README.md#download-and-start).

Run commands from an extracted release folder. `./routevane` resolves the
binary (use `./routevane.exe` explicitly on Windows if needed). Contributors
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
./routevane preview --service example --target raw-json
```

The command performs a live, deadline-bounded DNS lookup. Its output is stable
for the same normalized observations and explicit cutoff; live DNS answers and
the wall-clock cutoff can naturally change between invocations.

Refresh, build, and inspect from the CLI:

```powershell
./routevane refresh --service example
./routevane build --target raw-json --service example
./routevane doctor
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
./routevane run --target raw-json --service example --interval 30m
```

Use `--catalog-dir` and `--data-dir` on `refresh`, `build`, `doctor`, and `run`
to select alternate roots. File and directory modes are restricted where POSIX permissions apply;
Windows `chmod` is not presented as an ACL isolation boundary.

### Curated domain feeds

Shipped lists declare third-party curation as `community`, not as the vendor's
official publication. `format: domain-list` collects v2fly domain names,
including names prefixed with `full:`, and Routevane uses them as suffix rules
(the named host **and its subdomains**). This is destination discovery, not an
exact translation of the upstream ruleset. Patterns and remote includes are
not expanded. Refresh reports `skipped_entries`; the list card shows the count
after reading a source that contained unsupported or invalid entries.

GitHub Copilot is separate from GitHub. Its upstream feed includes two
telemetry hosts tagged `@ads`; Routevane retains those destinations and does not
interpret that tag as a filtering policy. Twitch imports only the named CDN
hosts, not the whole `cloudfront.net` domain or provider networks. Kinopub
imports supported names only: its regular-expression entry is skipped, so the
list does not claim to cover every dynamically named CDN host. See
[the catalogue policy](adr/0015-external-lists-as-catalog-material.md).

## Manual Keenetic file build and import

Refresh every service whose current observations should enter the file, then
build the selected set. `--service` is repeatable and is sorted and deduplicated
before one all-or-nothing build:

```powershell
./routevane refresh --service youtube
./routevane refresh --service discord
./routevane build --target keenetic --service youtube --service discord --output ./data/artifacts
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
6. After import, verify the route count and test that representative list
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
./routevane refresh --service youtube
./routevane build --target openwrt --service youtube --output ./data/artifacts
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
list is removed from the profile.

Hardware acceptance on a physical OpenWrt device is **unverified** in this
repository; the option syntax follows the dnsmasq manual.

## MikroTik RouterOS address lists

The `mikrotik` target builds a RouterOS script that populates two firewall
address lists:

```powershell
./routevane build --target mikrotik --service youtube --output ./data/artifacts
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
./routevane build --target amnezia --service youtube --output ./data/artifacts
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
./routevane serve --port 8765 --catalog-dir ./catalog --data-dir ./data --open-browser
```

The command prints its server-owned origin, normally `http://127.0.0.1:8765`.
The embedded UI opens after it is ready; omit `--open-browser` to open it manually.
Create a profile, choose its lists and outputs, then refresh and build it.
The UI uses relative `/v1` requests. Mutating requests must use exact
`Content-Type: application/json` and `X-Routevane-Request: 1`; build never
performs an implicit network refresh.

Adding an output does not create a subscription. Its first successful build
returns the subscription URL once; a failed initial build records its bounded
reason and can be retried without losing a secret. Treat the URL as a bearer
secret: Routevane stores only a token ID and hash and cannot display it again.
The UI keeps it only in component memory, masks it until explicit reveal or
copy, and intentionally loses it on reload; it never writes it to profile state,
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

## Ordering lists and resolving overlaps

The order of selected lists decides which list owns a shared destination. In a
profile, move lists in the **In profile** rail: the list above wins. Saving the profile
stores that order for this profile and rebuilds its outputs. Reordering the default
priority in **Lists** affects the initial order of new profiles and forecasts only;
it does not change profiles that already exist.

If two selected lists contain the same destination, Routevane keeps it under the
higher list. If a lower list's narrower rule is wholly covered by a higher list,
the lower rule is omitted. A broader lower-priority network is kept whenever it
still contributes addresses that the higher list does not cover, so changing
priority never silently reduces the requested routing union. A list newly added
through a live category is placed after the profile's saved order.

Selected table rows use intersection tags to name every other selected list they
overlap. Use those tags to decide which list should move higher; you do not need
to edit hundreds of individual destinations. The tag summary is complete even
when Diagnostics shows only the first 100 detailed overlap records.
See [ADR 0036](adr/0036-route-local-list-priority.md) for the ownership rules.

## Moving configuration to another computer

Open **Settings → Configuration transfer** on the source computer and download
the JSON file. On a fresh Routevane installation, select that file, review the
preview, and apply it. The destination must be empty; transfer does not merge
with an installation that already has profiles, connections, or custom lists.

The current `config-transfer-v1.4` file contains the global refresh setting, the
library's default list priority, custom lists and categories, catalog-source
on/off choices, per-destination source corrections, profiles with their own
priority, non-secret connection details, and output bindings. It does not contain
router passwords, bearer subscription tokens, operator-added HTTP source addresses,
published artifacts, observations, history, backups, logs, or browser
preferences. A preview warning tells you when custom sources were omitted; add
them again after transfer. Local catalog discoveries are also excluded because
another installation may not have the same files.

After import, enter each device password again, publish every output to create a
new artifact and subscription, and then enable automatic delivery where wanted.
Imported devices always start with automatic delivery off. Preview and apply
validate the same file digest; editing or replacing the file requires a new
preview. A file exported by an earlier version still imports: the fields ADR 0039
renamed are read under their retired names for one minor version. A file
naming one field under both names is refused rather than reconciled.
This is a portable settings transfer, not a database backup. See
[`ADR 0033`](adr/0033-portable-configuration-transfers.md).

## Adding a service by URL

```powershell
./routevane discover --url shop.example.co.uk
```

The first call performs no browser load. It prints the canonical HTTPS URL, the
registrable domain the Public Suffix List derived, and the local service identity
it would create. Repeat the call with `--confirm` to run exactly that URL:

```powershell
./routevane discover --url shop.example.co.uk --confirm --service-id shop --title Shop --browser "C:/Program Files/Google/Chrome/Application/chrome.exe"
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
./routevane learn --scenario learning-scenario.yaml
./routevane learn --scenario learning-scenario.yaml --confirm --browser "C:/Program Files/Google/Chrome/Application/chrome.exe"
```

A captured archive can be imported instead of driving a browser:

```powershell
./routevane learn --har session.har --url app.example.co.uk --confirm
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
./routevane deploy --artifact ID --target keenetic --device http://192.168.1.1 --user admin --interface Wireguard0
$env:ROUTEVANE_DEVICE_PASSWORD = Read-Host -AsSecureString | ConvertFrom-SecureString -AsPlainText
./routevane deploy --artifact ID --target keenetic --device http://192.168.1.1 --user admin --interface Wireguard0 --confirm
```

The first call changes nothing. A confirmed deployment probes the firmware,
refuses an incompatible one before touching the device, stores and verifies a
configuration backup, reconciles the artifact's exact persisted route claims on
the named interface, reads the device's route table back, and rolls back from
that backup if deployment or verification fails. Routes Routevane did not create
are preserved even on the same interface. A pre-existing desired route remains
foreign, shared prefixes remain until their last output claim leaves, and a
repeated deployment changes nothing. Missing ownership history is additive and
never guesses from interface membership. Every step is recorded with its outcome
and duration; no credential appears in the record. See
[`ADR 0032`](adr/0032-persist-exact-keenetic-static-route-ownership.md).

For a route Routevane creates, Keenetic's **Description** shows its provenance as
`(category/list)`. An uncategorized list uses `(Без категории/list)`. When a
prefix comes from several lists, the lexicographically first label is followed
by `+N`; the complete label set remains in the plan snapshot. Routevane reads
this value back and rolls back if Keenetic omits or changes it. A pre-existing same-prefix
route and its description remain untouched. Downloaded BAT files deliberately
stay unchanged; this description applies to automatic RCI delivery. See
[`ADR 0034`](adr/0034-keenetic-route-descriptions-from-plan-provenance.md).

Keenetic rollback uploads the complete captured configuration, not a scoped
route delta. Avoid concurrent router changes during delivery: unrelated changes
made after the backup may also be undone. Physical recovery remains unverified.
Use an interface ID such as `Wireguard0`, not its description. For DNS-based
delivery, an exclusive (`reject=true`) or ambiguously attached Routevane group
is refused before reconciliation; an omitted `reject` or `reject=false` is
accepted. No automatic conversion of the router's routing policy is performed.

For unattended delivery, register the device under **Connections** with every
non-secret connection field its deployer requests (Keenetic includes the route
interface), then turn on automatic delivery. A router password is stored only in
the operating system credential store; a local target such as sing-box needs no
password and creates no credential entry. On the profile's **Connection** tab,
bind the matching output to that explicit device and set the profile to daily or
weekly refresh. The scheduler then:

1. refreshes the profile and rebuilds each output independently;
2. considers only an artifact successfully published by that same run;
3. delivers it only to the device explicitly bound to that output and opted in;
4. uses the same probe, compatibility check, verified backup, deploy, verify,
   and rollback lifecycle as a manual send.

A failed format is not delivered from an older file, a failed device does not
stop sibling outputs, and the previously published artifact remains available.
Disabling automatic delivery deletes the stored credential; forgetting the
device detaches it from outputs without deleting their files or subscriptions
and retires its route-ownership scope without changing the router.
The consent, binding, and credential-free target rules are recorded in
[`docs/adr/0031-explicit-scheduled-device-delivery.md`](adr/0031-explicit-scheduled-device-delivery.md).

The Keenetic RCI interface, including its route `comment` property, is not covered
by a vendor specification. The deployer is verified against a faithful device
double and **has not been accepted against a physical router in this repository's
environment**. It refuses what it cannot confirm rather than guessing. The
boundary and its verification limits are recorded in
[`docs/adr/0008-device-deployment-boundary.md`](adr/0008-device-deployment-boundary.md)
and [`ADR 0034`](adr/0034-keenetic-route-descriptions-from-plan-provenance.md).

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
./routevane deploy --artifact ID --target singbox --device file:///C:/sing-box/config.json
./routevane deploy --artifact ID --target singbox --device file:///C:/sing-box/config.json --confirm
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
