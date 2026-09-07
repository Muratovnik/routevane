<p align="center">
  <img src="web/public/routevane-logo.svg" alt="" width="72">
</p>
<h1 align="center">Routevane</h1>
<p align="center">Routing lists for your router or VPN client, built on your computer.</p>
<p align="center"><b>English</b> · <a href="README.ru.md">Русский</a></p>
<p align="center">
  <a href="https://github.com/Muratovnik/routevane/releases/latest"><img src="https://img.shields.io/github/v/release/Muratovnik/routevane?label=release" alt="Latest release"></a>
  <a href="https://github.com/Muratovnik/routevane/actions/workflows/ci.yml"><img src="https://github.com/Muratovnik/routevane/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Muratovnik/routevane" alt="MIT license"></a>
</p>

Routevane builds routing lists for a router or VPN client. Choose lists and
categories from a supplied library, combine them into a profile, and export it in
the format your device understands. Lists refresh manually or on a schedule.

> [!NOTE]
> Routevane prepares routing rules. It does not provide a VPN connection, and it
> needs no account or cloud service: everything runs on your computer.

- **Ready-made lists.** Popular services grouped into categories. Add your own
  lists, or discover one from its URL with a browser.
- **Profiles instead of spreadsheets.** Combine lists and categories into a profile.
  List priority resolves overlapping destinations, and a forecast shows how many
  rules each format would build before you build it.
- **Five output formats.** Keenetic (IPv4 routes and FQDN groups), sing-box,
  OpenWrt, MikroTik, and AmneziaVPN. One profile can publish several formats.
- **Safe updates.** Every published file is validated first. If a scheduled
  rebuild fails, the previous valid file stays available.
- **Explicit delivery.** Subscription links work on this computer only. Sending
  rules to a Keenetic router or a local sing-box is a separate action that you
  turn on yourself.
- **Local and bilingual.** A desktop application for Windows, a command-line
  package for Windows, Linux, and macOS, and an interface in English and Russian.

## Download and start

Every release on the [releases page](https://github.com/Muratovnik/routevane/releases/latest)
ships the files below. Download from **Assets**, not the **Source code** archive.
`SHA256SUMS` in the same release lets you [verify a download](docs/installation.md#verify-a-download).

| Distribution | Platform | File |
| --- | --- | --- |
| Desktop application | Windows x64 | `Routevane-X.Y.Z-x64-setup.exe` |
| CLI package | Windows x64 | `routevane-vX.Y.Z-windows-amd64.zip` |
| CLI package | Windows ARM64 | `routevane-vX.Y.Z-windows-arm64.zip` |
| CLI package | Linux x64 | `routevane-vX.Y.Z-linux-amd64.zip` |
| CLI package | Linux ARM64 | `routevane-vX.Y.Z-linux-arm64.zip` |
| CLI package | macOS 13 or newer, Apple silicon | `routevane-vX.Y.Z-darwin-arm64.zip` |

Neither distribution needs Go, Node.js, Python, or build tools. There are no
packages for Intel Macs or 32-bit systems.

### Desktop application (Windows)

1. Download `Routevane-X.Y.Z-x64-setup.exe` and run it. The installation is per
   user and needs no administrator rights.
2. The installer is not signed yet, so Windows SmartScreen can warn about an
   unknown publisher. Verify the checksum first, then choose **More info →
   Run anyway**.
3. Routevane opens in its own window.

Closing the window hides Routevane to the tray, and scheduled refreshes keep
running. **Open Routevane** in the tray menu restores the window; **Quit** stops
the application. When a newer version is published, **Update** appears at the
bottom of the left menu: one click downloads, installs, and restarts. Your
profiles and settings live in `%APPDATA%\Routevane\`. See
[desktop installation, updates, and backups](docs/installation.md#desktop-application-windows).

### CLI package (Windows, Linux, macOS)

The package runs Routevane as a local service and opens the interface in your
browser.

1. Download the ZIP for your platform and extract the whole folder.
2. Start it. On Windows, double-click **`start-routevane.cmd`**. On Linux and
   macOS, open a terminal in the folder and run:

   ```sh
   sh ./start-routevane.sh
   ```

3. Open **http://127.0.0.1:8765** if the browser does not open by itself.

Keep the console window open while you work; press **Ctrl+C** in it to stop.
Your data is created in `data/` beside the launcher. Keep the `routevane`
executable together with the launcher and `catalog/`; double-clicking it alone
shows command-line help instead of opening the interface. See
[launch options, updates, and backups](docs/installation.md#cli-package-windows-linux-macos).

## Build your first profile

Routevane works with four things. A **list** is a named set of destinations:
domains, addresses, networks. A **category** groups lists. A **profile** combines
lists and categories. A **connection** publishes a profile in one format,
optionally to one device, and owns that format's subscription link.

1. Open **Lists** to see the supplied library.
2. Open **Profiles** and choose **Build a profile**. Select lists or whole
   categories and pick the format of your router or application. The forecast
   beside each format shows how many rules it would build.
3. After the first successful build, save the subscription link while it is
   shown, or download the file from **Connections**.

> [!WARNING]
> The secret in a subscription link is shown once and cannot be displayed again.
> Subscription links work only on the computer running Routevane; a router
> cannot fetch them.

A profile can publish several formats. If a scheduled update fails, the last valid
file stays available. Sending rules to a device is a separate, explicit action;
automatic delivery stays off until you enable it in **Connections**.

## Supported outputs

| Output | How it reaches the device | Verified so far |
| --- | --- | --- |
| Keenetic IPv4 routes | BAT file import, or delivery to the router | Protocol tests; no physical router yet |
| Keenetic FQDN groups (KeeneticOS 5.0+) | CLI import, or delivery to the router | Protocol tests; no physical router yet |
| sing-box source rule set | JSON file, or delivery to a local sing-box | File validity; live reload unverified |
| OpenWrt dnsmasq nftset | Manual installation | Rendering; firewall integration unverified |
| MikroTik address lists | Manual script import | Rendering; device execution unverified |
| AmneziaVPN split-tunnel list | JSON import | Rendering; client import unverified |

Read the [format guide and limitations](docs/usage.md#supported-outputs) before
applying rules to a device.

## Status

Routevane is at version `0.y.z`: a breaking change raises the minor version,
everything else raises the patch. Rendering and validation of every format, the
browser interface, the Windows desktop lifetime, and installer updates are
covered by automated tests on every release. Not yet verified: operation on
physical Keenetic, OpenWrt, MikroTik, and AmneziaVPN devices, live sing-box
reload, and a signed installer. Desktop packages exist for Windows only. See
[open work and limits](docs/requirements.md#open-work-and-limits) and the
[changelog](CHANGELOG.md).

## Documentation

| Task | Document |
| --- | --- |
| Verify, update, back up, remove, or troubleshoot | [Installation and maintenance](docs/installation.md) |
| Move profiles and settings to another computer | [Configuration transfer](docs/usage.md#moving-configuration-to-another-computer) |
| Use the CLI, manual imports, or browser-assisted discovery | [Advanced usage](docs/usage.md) |
| Install or write a plugin | [Plugin examples](examples/plugins/README.md) · [SDK](sdk/routevaneplugin/README.md) |
| Build from source and contribute | [Contributing](CONTRIBUTING.md) |
| Read the user guides in Russian | [Документация на русском языке](docs/ru/README.md) |
| Everything else, by task | [Documentation index](docs/README.md) |

## Help

- Questions and bug reports go to [GitHub Issues](https://github.com/Muratovnik/routevane/issues).
  Include the Routevane version, your operating system, whether you use the
  desktop application or the CLI package, and the exact error. Never post your
  database, subscription links, or device passwords.
- Vulnerabilities are reported privately as described in the
  [security policy](SECURITY.md).

## License

[MIT](LICENSE)
