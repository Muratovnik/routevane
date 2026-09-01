# Routevane

Routevane creates and updates routing lists for routers and client applications.
Choose the lists you need, combine them into a route, and get a validated file,
a subscription link, or delivery to a supported device. It runs locally, without
an account or cloud service. The interface is available in English and Russian.

## Download and start

Get the archive for your computer and `SHA256SUMS` from
[Releases](https://github.com/Muratovnik/routevane/releases).
If no release is published yet, there is no ready-to-run download; developers
can [build from source](CONTRIBUTING.md).

| Computer | Archive suffix |
| --- | --- |
| Windows x64 | `windows-amd64.zip` |
| Windows ARM64 | `windows-arm64.zip` |
| Linux x64 | `linux-amd64.zip` |
| Linux ARM64 | `linux-arm64.zip` |
| macOS ARM64, macOS 13+ | `darwin-arm64.zip` |

The release needs no Go, Node.js, Python, or developer setup.
macOS Intel, 32-bit systems, containers, and service-manager installation are
not included in the release matrix.

1. Check the download against the matching entry in `SHA256SUMS`. On Windows,
   from the download directory (substitute your archive name):

   ```powershell
   $Archive = 'routevane-v0.1.0-windows-amd64.zip'
   $Expected = (Get-Content ./SHA256SUMS | Where-Object { $_.EndsWith("  $Archive") })
   if (@($Expected).Count -ne 1) { throw 'Missing or ambiguous checksum entry' }
   $Actual = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
   if ($Actual -ne $Expected.Substring(0, 64)) { throw 'Checksum mismatch; do not run this archive' }
   'Checksum verified'
   ```

   On Linux use `sha256sum --ignore-missing -c SHA256SUMS`.
   On macOS run `shasum -a 256 routevane-v0.1.0-darwin-arm64.zip` and compare
   its hash with the line for that exact filename in `SHA256SUMS`.
   Keep the checksum file and archive from the same release.
   Checksums detect changed bytes; [provenance verification](docs/releasing.md#verify-a-download)
   also checks the signed build identity.

2. Extract the entire archive into a writable folder. It contains:

   - `start-routevane.cmd` (Windows) or `start-routevane.sh` (Linux/macOS): **start here**;
   - `routing-agent.exe` or `routing-agent`: the application used by the launcher;
   - `catalog/`: the supplied lists and formats;
   - `README.txt`: offline start, stop, update, and recovery instructions;
   - `LICENSE`, `THIRD_PARTY_NOTICES.txt`, `SBOM.spdx.json`: licensing and dependency information.

3. On Windows double-click **start-routevane.cmd**. On Linux/macOS run
   **`sh ./start-routevane.sh`**. The launcher starts Routevane and requests a
   browser page after the local UI is ready. If your computer has no browser
   opener, open `http://127.0.0.1:8765` yourself.

The console stays open while the application runs. Press Ctrl+C to stop it.
To choose another port, run `./start-routevane.cmd 9000` or
`sh ./start-routevane.sh 9000`. The UI is available only on this computer;
a router cannot reach your computer's loopback subscription address directly.

## First use

Open **Lists** to inspect the supplied library without changing anything.
Then open **Routes**, choose **Build a route**, select lists or categories,
and choose the first connection format.

A route keeps your selection and can publish it in several formats. Its
**Contents**, **Connection**, **File**, and **Diagnostics** tabs separate editing,
delivery, exact file contents, and technical details. A failed rebuild keeps
the previous validated file available.

A subscription link is shown once after the first successful publication.
Copy it when offered: refreshing the page does not recover its secret.
Only configure device delivery when you intend to change that device.
Unattended delivery requires an explicit device binding and opt-in consent.

## Supported outputs

“Built-in” means the format is rendered and validated by repository tests.
It is not a claim that a physical device or third-party client was exercised.

| Output | Use and acceptance limit |
| --- | --- |
| Keenetic IPv4 routes | BAT import or device delivery; protocol tests, no physical-router acceptance |
| Keenetic FQDN groups | KeeneticOS 5.0+ CLI import or delivery; protocol tests, no physical-router acceptance |
| sing-box source rule set | JSON file or atomic local-file delivery; live runtime reload is unverified |
| OpenWrt dnsmasq nftset | Manual installation; physical firewall integration is unverified |
| MikroTik address lists | Manual script import; device execution and interruption recovery are unverified |
| AmneziaVPN split-tunnel list | JSON import; client import acceptance is unverified |

Raw JSON is a diagnostic format. [Advanced usage](docs/usage.md) describes
format prerequisites, manual imports, discovery, and CLI operations.
The browser suite covers Chromium; every system browser is not separately accepted.

## Your data, updates, and removal

Routevane creates `data/` beside the launcher. It contains your database,
publication history, and generated artifacts. Keep it private and back it up
with Routevane stopped. Do not delete it as a cache.

To update, stop Routevane, back up `data/` and any catalog edits, verify and
extract the new archive into a separate folder, then copy `data/` there.
Review and merge your catalog edits instead of replacing a new catalog with
an old one. Retain the old folder and stopped-state backup until the new
version works. To roll back, restore that backup with the old version; do not
open a migrated database with an older binary.

To remove the application, stop it and remove its extracted folder. This also
removes its local data unless you saved it elsewhere. Disable opted-in device
credentials in Connections first so the operating system's secret-store entries
are removed. Removing Routevane does not undo routes already applied to a device.

## Troubleshooting

- **The page does not open:** keep the console open, read its error, and try the
  printed address. Confirm `./routing-agent version` matches your download and
  `http://127.0.0.1:8765/health` responds with `status: ok`.
- **Port already in use:** stop the known Routevane instance or use another port.
  Do not terminate an unidentified process.
- **Missing binary or catalog:** extract the whole archive; do not move only the launcher.
- **A request for Go, Node, or Python:** you have the source checkout, not the
  release archive. Use Releases or follow the developer guide.
- **Lost subscription link:** the secret cannot be recovered; creating another
  connection produces a new subscription after publication. It does not revoke
  the old link. Token rotation/revocation is not currently implemented.

Report vulnerabilities through [the private security channel](SECURITY.md).
For development, use [CONTRIBUTING](CONTRIBUTING.md); for other documentation,
use the [documentation index](docs/README.md).
