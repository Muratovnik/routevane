---
status: adopted
---

# ADR 0038: Windows application updates from GitHub releases

## Context

The desktop must offer a new version without requiring users to replace an
application directory. Running the downloaded installer manually must also
preserve the same data. The CLI remains independently distributed.

## Decision

Use electron-builder's per-user NSIS installer and electron-updater. This replaces
the Packager choice in ADR 0037 with one owner for directory packaging, installers,
update metadata, download verification and replacement. Electron's built-in Windows
updater uses a different installer/update format; a custom downloader would duplicate
installer lifecycle, caching and integrity handling. No additional Go service is needed.

Production packages use the public GitHub provider for `Muratovnik/routevane`.
Builds never publish. The existing release workflow uploads the versioned installer,
blockmap, `latest.yml` and desktop checksums in the complete draft before publication.
Desktop provenance is attested separately from the existing CLI archive SBOM.
Only stable upgrades are offered; no downgrade or prerelease channel is enabled.

The main process checks shortly after startup, every four hours, and on resume.
Discovery never downloads. A sidebar click downloads the installer, reports progress,
then stops the owned Go process before calling `quitAndInstall` with restart enabled.
A failed download keeps the current application usable and offers retry. Normal Quit
never installs a cached download. Close-to-tray keeps the existing background work
and discovery active, but does not authorize installation.

The renderer receives state, subscription and apply operations only. IPC validates
the app window's main frame; it exposes no remote URL or filesystem argument.
Only installed Windows builds enable checking. NSIS writes the install marker;
source and unpacked directory launches have no update affordance. Tests compile an
isolated installer identity and loopback feed, never a runtime production override.

NSIS is installed per user without elevation. Both manual and automatic updates use
the same installer identity and keep the user-data directory outside application
files. The installer does not delete data on uninstall. This does not migrate a CLI
profile or turn the CLI into a desktop dependency.

## Limits and verification

Windows x64 is the accepted installer target. Code-signing credentials are not
configured; downloads rely on HTTPS GitHub release metadata and the updater's
SHA-512 verification. Hashes do not establish publisher identity if the release
account is compromised. Windows may show an unknown-publisher prompt for unsigned
installers. Adding signing must retain the same installation identity and updater.

`tools/dev.ps1 test-update` drives real fixture installers through manual upgrade,
a corrupt download followed by retry, automatic replacement and restart, and
preserved routes/preferences. Native desktop lifecycle, canonical and browser gates
remain required. Local acceptance does not prove a public release was published.

- [electron-builder auto update](https://www.electron.build/docs/features/auto-update/)
- [electron-builder NSIS](https://www.electron.build/nsis.html)
- [Electron built-in autoUpdater](https://www.electronjs.org/docs/latest/api/auto-updater)
