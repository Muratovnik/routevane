---
status: adopted
---

# Installation and maintenance

Audience: users of the released application. The [quick start](../README.md#download-and-start)
covers the first launch; this guide covers verification, launch options,
updates, backups, removal, and recovery for both distributions. Building from
source is a separate [developer workflow](../CONTRIBUTING.md).

Neither distribution needs Go, Node.js, Python, or PowerShell 7. Minimum
operating system versions follow the Go toolchain that builds Routevane: the
macOS package needs macOS 13 or newer. There are no packages for Intel Macs or
32-bit systems, and no official container or service-manager installer.
Automated interface tests cover Chromium; other browsers have not been
separately verified.

## Verify a download

Download `SHA256SUMS` from the same release as your file. It lists the expected
SHA-256 hash of every archive and of the Windows installer. These checks use
system utilities, not developer tools. Replace the example filename with the
file you downloaded.

On Windows, open PowerShell in the download folder:

```powershell
(Get-FileHash ./Routevane-X.Y.Z-x64-setup.exe -Algorithm SHA256).Hash
```

Open `SHA256SUMS` in a text editor and compare the full hash with the entry for
that exact filename. Uppercase and lowercase letters are equivalent.

On Linux, with the ZIP and `SHA256SUMS` in the current directory:

```sh
sha256sum --ignore-missing -c SHA256SUMS
```

Confirm your archive is listed as `OK`. On macOS:

```sh
shasum -a 256 routevane-vX.Y.Z-darwin-arm64.zip
```

Compare the full hash with the entry for that exact filename in `SHA256SUMS`.
If the entry is missing or the hash differs, do not run the file. Checksums
detect changed bytes. For the additional signed build-identity check, see
[provenance verification](releasing.md#verify-a-download); GitHub CLI is needed
only for that check, not to run Routevane.

## Desktop application (Windows)

### Install

When a release provides `Routevane-X.Y.Z-x64-setup.exe`, run it. The
installation is per user and needs no administrator rights or separate browser.
Releases without that file contain only the CLI package.

The installer is not signed yet, so Windows SmartScreen can report an unknown
publisher. Verify the checksum first, then choose **More info → Run anyway**.

Routes, connections, and display preferences are stored in
`%APPDATA%\Routevane\`, with the database under its `data\` folder, outside the
installation folder. Closing the window hides Routevane to the tray; **Quit**
in the tray menu stops it.

### Update

An installed application checks [GitHub releases](https://github.com/Muratovnik/routevane/releases)
for a newer stable version shortly after it starts, every four hours, and after
the computer wakes from sleep. When one exists, **Update** appears at the bottom
of the left menu. Save your edits, then click it: Routevane downloads the
installer, installs it, and restarts. A failed download leaves the current
version running and offers a retry. Nothing downloads or installs just because
you close the window or quit.

Alternatively, download the new installer from the release page, choose
**Quit** in the tray, and run it. Do not uninstall first; your data stays in
`%APPDATA%\Routevane\`.

### Back up and restore

Choose **Quit** in the tray before copying data; closing the window only hides
it. Copy the whole `%APPDATA%\Routevane\` folder. To restore, install the same
or a newer version, quit it, replace the folder with your copy, and start
Routevane again. Do not open a database migrated by a newer version with an
older one.

Data from the CLI package is not imported automatically. To move routes from a
CLI installation, use [configuration transfer](usage.md#moving-configuration-to-another-computer),
enter device passwords again, and publish each connection anew.

### Remove

Before removing Routevane, disable automatic delivery for your devices in
**Connections** so stored passwords are removed from the Windows credential
store. Then quit Routevane and uninstall it from **Settings → Apps**. The
uninstaller keeps `%APPDATA%\Routevane\`; delete that folder yourself if you do
not want to keep your routes. Removing Routevane does **not** undo routes
already applied to a device.

## CLI package (Windows, Linux, macOS)

### What the download contains

Download the ZIP for your operating system from [Releases](https://github.com/Muratovnik/routevane/releases/latest),
not GitHub's **Source code** archive. Extract the entire application folder:

| File | Purpose |
| --- | --- |
| `start-routevane.cmd` or `start-routevane.sh` | Starts the application and opens its browser interface |
| `routing-agent.exe` or `routing-agent` | The application, including its web interface and command-line program |
| `catalog/` | Supplied lists and output definitions |
| `README.txt` | Offline start, stop, update, and recovery instructions |
| `LICENSE`, `THIRD_PARTY_NOTICES.txt`, `SBOM.spdx.json` | License and bundled dependency information |

Keep these files together. The launcher uses the extracted folder for both the
catalog and your data, regardless of where you start it from. It does not
install a system service or arrange startup at login. Windows runs the `.cmd`
launcher with its built-in command processor; Linux and macOS use `sh`. Use
your existing browser for the interface. Optional
[browser-assisted discovery](usage.md#adding-a-service-by-url) needs an
explicitly configured Chromium-family browser; it is not a prerequisite for
starting the application or using the supplied lists.

### Launch options

To choose another port, run from the extracted application folder:

```powershell
./start-routevane.cmd 9000
```

On Linux or macOS:

```sh
sh ./start-routevane.sh 9000
```

Then open `http://127.0.0.1:9000`. Setting `ROUTEVANE_NO_BROWSER=1` before
launch suppresses automatic browser opening. To run the binary without the
launcher, see [the direct CLI command](usage.md#local-subscription-service).

The interface and subscriptions listen only on this computer. A router or
another computer cannot fetch a `127.0.0.1` subscription URL from it. Use file
export or an available delivery integration for those devices; do not expose
the local service to the network as a workaround.

### Update and back up

Routevane creates `data/` beside the launcher. It contains your database,
publication history, and generated files. Keep it private; it is not a cache.

1. Stop Routevane with Ctrl+C before backing up its data.
2. Back up `data/` and any edits you made under `catalog/`.
3. Verify and extract the new release into a separate folder.
4. Copy `data/` to the new folder. Review and merge catalog edits instead of
   overwriting the new catalog with the old one.
5. Start the new version and check your routes. Keep the old folder and the
   stopped-state backup until you are satisfied it works.

To roll back, use the old version with the backup taken before the update. Do
not open a database migrated by a newer version with an older binary.

### Remove

Before removing it, disable automatic delivery for your devices in
**Connections** so stored passwords are removed from the operating system's
credential store. Then stop Routevane, save any data you want to keep, and
delete its extracted folder. Deleting the folder also deletes its local data.
It does **not** undo routes already applied to a device.

## Troubleshooting

- **The page does not open (CLI package):** keep the console open and read its
  error. Try the printed address manually. From the extracted folder, run
  `./routing-agent version` (`./routing-agent.exe version` on Windows) to check
  the version, and open `http://127.0.0.1:8765/health` to check for
  `status: ok`. Use your chosen port if it differs from the default.
- **Routevane could not start, or its backend stopped (desktop):** another
  program may be using its data folder or port. Quit any other Routevane
  instance, then start it again.
- **Port already in use:** stop a known Routevane instance or choose another
  port. Do not terminate an unidentified process.
- **The EXE flashes a console and exits:** use `start-routevane.cmd`. Running
  the executable without a command shows CLI help, not the browser interface.
- **Missing binary or catalog:** extract the whole archive; do not move only
  the launcher or the executable.
- **Instructions ask for Go, Node.js, or Python:** those are source-development
  instructions. Download the release for the ready-built application.
- **An update fails to download:** the current version keeps running. Click
  **Update** again later, or install the new version manually from the
  release page.
- **Lost subscription link:** its secret cannot be recovered. Creating another
  connection produces a new link after publication but does not revoke the old
  one. Token rotation and revocation are not currently implemented.

For a bug report, include the version, operating system, distribution (desktop
application or CLI package), and the relevant error. Do not post your database,
subscription URLs, or device passwords. Report vulnerabilities through the
[private security channel](../SECURITY.md).
