# Developing Routevane

This guide is for building and changing the source checkout. To run a downloaded
application, use the [user guide](README.md).

## Prerequisites

- Go 1.27.x, as declared in [go.mod](go.mod).
- Node.js 24.19+ within the 24.x line. [.node-version](.node-version) pins the
  version used by CI; [web/package.json](web/package.json) declares the minimum.
- npm 11.17+ within the 11.x line.
- PowerShell 7.4+ on every development OS; Windows PowerShell 5.1 is not supported.
- Python 3.11+ and Git.

On Ubuntu with AppArmor user-namespace restrictions, the downloaded Chromium
also needs a working sandbox. If a trusted Google Chrome installation already
provides its root-owned SUID helper, set the following before `test-browser`:

```powershell
$env:CHROME_DEVEL_SANDBOX = '/opt/google/chrome/chrome-sandbox'
```

Otherwise follow the [Chromium sandbox setup](https://chromium.googlesource.com/chromium/src/+/main/docs/security/apparmor-userns-restrictions.md).
Do not disable the browser sandbox or AppArmor globally. On its disposable
GitHub-hosted VM, CI instead loads an in-memory AppArmor rule for the exact
pinned Chromium executable. The CI helper refuses to run on a developer host.

## First checkout

Run from the repository root:

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 setup
pwsh -NoLogo -NoProfile -File tools/dev.ps1 install-hooks
pwsh -NoLogo -NoProfile -File tools/dev.ps1 check
pwsh -NoLogo -NoProfile -File tools/dev.ps1 setup-browser
pwsh -NoLogo -NoProfile -File tools/dev.ps1 test-browser
```

`setup` validates prerequisites before downloading Go/npm dependencies. It does
not install Git hooks or browser binaries. `install-hooks` installs the tracked
checkout-local hooks. `setup-browser` installs the pinned Chromium in the
checkout's ignored `.cache/browsers/`; on Linux it can also need elevated
package-manager permission for browser system libraries. No user browser profile
is used. Do not approve arbitrary dependency install scripts: approvals are
version-pinned in package.json.

A deprecated transitive npm package warning is not an instruction to install a
different version or approve scripts. Investigate an audit failure; the security
gate must remain green.

## Run and build

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 up
pwsh -NoLogo -NoProfile -File tools/dev.ps1 build
```

`up` builds the embedded UI and binary, starts the service, and opens the page.
Use `-Port 9000` or `-NoBrowser` when needed; stop with Ctrl+C.
On Windows, **dev.cmd** is a convenience shortcut for this source-build path,
not a release launcher.

The binary is written to `.cache/build/`. Runtime state goes to `data/`;
reports and browser binaries go below `.cache/`, and browser test artifacts
below `tmp/`. Preserve runtime data when cleaning generated files.
Plain `go build` does not generate the UI and can produce an API-only binary.

## Before changing code

Read [the repository contract](AGENTS.md), then the relevant
[current requirements](docs/requirements.md),
[architecture](docs/ARCHITECTURE.md), or [UI contract](docs/UI.md).
Historical plans explain earlier choices; they are not current setup instructions.

Work one user-visible slice at a time and preserve unrelated changes. Verify
the rebuilt runtime identity, not a previously running binary.
The complete gate is `tools/dev.ps1 check`; browser-facing changes also require
`tools/dev.ps1 test-browser`. Linux CI additionally runs the race detector.
The gates validate code and mechanical repository contracts; they do not replace
the file/audience and end-user review required for a prerelease.

Commits follow the [repository Git contract](AGENTS.md#git). Stage only owned
paths, run the staged publication audit, and keep the index empty after committing.

- [CLI and format details](docs/usage.md)
- [Plugin examples and installation](examples/plugins/README.md)
- [Adding a renderer](docs/adding-a-renderer.md)
- [Release procedure](docs/releasing.md)
- [Security reporting](SECURITY.md)
