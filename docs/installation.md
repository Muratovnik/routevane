---
status: adopted
---

# Installation and maintenance

The instructions below cover the independently distributed CLI/browser package.
The Electron desktop package has a [separate source build](../CONTRIBUTING.md#desktop-application).
Its Close button hides to the tray; Quit stops the owned backend. Desktop data
is under the operating system's Routevane user-data directory, outside the
installation folder. Quit before backup or replacement; keep the entire package
together. Signed installers and automatic application updates are not available yet.

For users of the downloaded application. Follow the [quick start](../README.md#download-and-start)
for your first launch; this guide covers verification, launch options, updates,
and recovery. Building the source is a separate [developer workflow](../CONTRIBUTING.md).

## What the download contains

Download the ZIP for your operating system from [Releases](https://github.com/Muratovnik/routevane/releases/latest),
not GitHub's **Source code** archive. Extract the entire application folder:

| File | Purpose |
| --- | --- |
| `start-routevane.cmd` or `start-routevane.sh` | Starts the application and opens its browser interface |
| `routing-agent.exe` or `routing-agent` | The application, including its web interface |
| `catalog/` | Supplied lists and output definitions |
| `README.txt` | Offline start, stop, update, and recovery instructions |
| `LICENSE`, `THIRD_PARTY_NOTICES.txt`, `SBOM.spdx.json` | License and bundled dependency information |

Keep these files together. The launcher uses the extracted folder for both the
catalog and your data, regardless of where you start it from. It does not
install a system service or arrange startup at login.

The normal release workflow needs no Go, Node.js, Python, or PowerShell 7.
Windows runs the `.cmd` launcher with its built-in command processor; Linux and
macOS use `sh`. Use your existing browser for the interface. Optional
[browser-assisted discovery](usage.md#adding-a-service-by-url) needs an explicitly
configured Chromium-family browser; it is not a prerequisite for starting the app
or using the supplied lists.

There are no release packages for Intel Macs or 32-bit systems, nor an official
container or service-manager installer. Automated interface tests cover Chromium;
other system browsers have not been separately verified.

## Launch options

To choose another port, run from the extracted application folder:

```powershell
./start-routevane.cmd 9000
```

On Linux or macOS:

```sh
sh ./start-routevane.sh 9000
```

Then open `http://127.0.0.1:9000`. Setting `ROUTEVANE_NO_BROWSER=1` before launch
suppresses automatic browser opening. To run the binary without the launcher,
see [the direct CLI command](usage.md#local-subscription-service).

The UI and subscriptions listen only on this computer. A router or another
computer cannot fetch a `127.0.0.1` subscription URL from it. Use file export or
an available delivery integration for those devices; do not expose the local
service to the network as a workaround.

## Verify a download

Download `SHA256SUMS` from the same release as your ZIP. It lists the expected
SHA-256 hash for each archive. These checks use system utilities, not developer
tools. Replace the example filename with the archive you downloaded.

On Windows, open PowerShell in the download folder:

```powershell
(Get-FileHash ./routevane-v0.1.0-windows-amd64.zip -Algorithm SHA256).Hash
```

Open `SHA256SUMS` in a text editor and compare the full hash with the entry for
that exact filename. Uppercase and lowercase letters are equivalent.

On Linux, with the ZIP and `SHA256SUMS` in the current directory:

```sh
sha256sum --ignore-missing -c SHA256SUMS
```

Confirm your archive is listed as `OK`. On macOS:

```sh
shasum -a 256 routevane-v0.1.0-darwin-arm64.zip
```

Compare the full hash with the entry for that exact filename in `SHA256SUMS`.
If the entry is missing or the hash differs, do not run the archive.
Checksums detect changed bytes. For the additional signed build-identity check,
see [provenance verification](releasing.md#verify-a-download); GitHub CLI is
needed only for that check, not to run Routevane.

## Updates and backups

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

## Remove Routevane

Before removing it, disable automatic delivery for your devices in **Connections**
so stored passwords are removed from the operating system's credential store.
Then stop Routevane, save any data you want to keep, and delete its extracted
folder. Deleting the folder also deletes its local data. It does **not** undo
routes already applied to a device.

## Troubleshooting

- **The page does not open:** keep the console open and read its error. Try the
  printed address manually. From the extracted folder, run `./routing-agent version`
  (`./routing-agent.exe version` on Windows) to check the version, and open
  `http://127.0.0.1:8765/health` to check for `status: ok`. Use your chosen port
  if it differs from the default.
- **Port already in use:** stop a known Routevane instance or choose another
  port. Do not terminate an unidentified process.
- **The EXE flashes a console and exits:** use `start-routevane.cmd`. Running
  the EXE without a command shows CLI help, not the browser interface.
- **Missing binary or catalog:** extract the whole archive; do not move only
  the launcher or EXE.
- **Instructions ask for Go, Node.js, or Python:** those are source-development
  instructions. Download the release ZIP to use the ready-built application.
- **Lost subscription link:** its secret cannot be recovered. Creating another
  connection produces a new link after publication but does not revoke the old
  one. Token rotation and revocation are not currently implemented.

For a bug report, include the version, operating system, and relevant error.
Do not post your database, subscription URLs, or device passwords. Report
vulnerabilities through the [private security channel](../SECURITY.md).
