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

`doctor` checks workflow YAML with the parser already pinned in `go.mod`; its
first run may populate the Go module/build caches. It needs no extra Python
package or installed frontend dependencies.

Client settings are not build prerequisites. Ignored preferences and personal
skills are not inspected as repository source. `.codex/config.toml` and
`.claude/settings.json` are ignored by default here, but reviewed portable shared
settings may be deliberately committed. Their project scope is documented by
[OpenAI](https://learn.chatgpt.com/docs/config-file/config-advanced#project-config-files-codexconfigtoml)
and [Anthropic](https://code.claude.com/docs/en/settings#share-settings-with-your-team).
Keep personal overrides, credentials, workstation paths, and internal reports
private. Publication audits still reject the private paths declared in `relkit.toml`;
an ignored local file does not belong in a source archive or public documentation link.

A deprecated transitive npm package warning is not an instruction to install a
different version or approve scripts. Investigate an audit failure; the security
gate must remain green.

## Run and build

For everyday development, run from the repository root:

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 dev
```

On Windows, double-click **dev.cmd** or run `./dev.cmd`. After dependency
setup, `npm run dev` from `web/` starts the same development session.
The UI opens at `http://127.0.0.1:8765`:

- Vue and CSS edits use Nuxt/Vite hot module replacement; no Go or static UI
  rebuild is needed. Template/style edits normally preserve the current draft.
- Go source, `go.mod`, `go.sum`, and catalog edits trigger a debounced backend
  rebuild/restart. Compilation errors stay in the terminal while the previous
  backend keeps running; saving a corrected file retries automatically.
  Reload or repeat the UI action to see changed backend behavior.
- The UI proxies `/v1` and `/health` to a separate, automatically selected
  loopback API port. Use the printed UI address, not the backend's embedded UI.
  The proxy checks the UI Host and Origin before translating them; production
  Host/Origin checks and CORS behavior are unchanged.
- Development data lives in `.cache/dev-data/`, separate from the ordinary
  application's `data/`. It survives restarts; preserve it if you want to keep
  your development routes when cleaning `.cache/`.
- Ctrl+C stops both owned servers. An occupied UI port is an error, not permission
  to reuse or terminate another process. Use `dev -Port 9000` or `./dev.cmd 9000`
  to choose a different port; `dev -NoBrowser` skips opening the browser.

The existing Python toolchain supervises the two processes and watches only Go
and catalog inputs; the pinned Nuxt/Vite toolchain owns frontend watching and
its [development proxy](https://vite.dev/config/server-options#server-proxy).
No additional watcher installation or production-only security exception is needed.
Stop the dev session before running build/check/browser gates, which also use
Nuxt's generated working directories. Dev mode is local-only, not a deployment.

To verify the shipped, embedded UI or build the standalone executable:

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 up
pwsh -NoLogo -NoProfile -File tools/dev.ps1 build
```

`up` builds the embedded UI and binary, starts the service, and opens the page.
It has no hot reload. Use `-Port 9000` or `-NoBrowser` when needed; stop with Ctrl+C.
Downloaded releases keep their separate `start-routevane.cmd` launcher.

The binary is written to `.cache/build/`. Runtime state goes to `data/`;
reports and browser binaries go below `.cache/`, and browser test artifacts
below `tmp/`. Preserve runtime data when cleaning generated files.
Plain `go build` does not generate the UI and can produce an API-only binary.

## Before changing code

Read [the repository contract](AGENTS.md), then the relevant
[current requirements](docs/requirements.md),
[architecture](docs/ARCHITECTURE.md), or [UI contract](docs/UI.md).

Work one user-visible slice at a time and preserve unrelated changes. Verify
the rebuilt runtime identity, not a previously running binary.
The complete gate is `tools/dev.ps1 check`; browser-facing changes also require
`tools/dev.ps1 test-browser`. This includes both the embedded-product browser
suite and `npm run test:dev`: real API reads/writes, HMR without losing a draft,
failed Go compilation/recovery, and process cleanup against throwaway data.
Linux CI additionally runs the race detector.
The gates validate code and mechanical repository contracts; they do not replace
the file/audience and end-user review required for a prerelease.

Commits follow the [repository Git contract](AGENTS.md#git). Stage only owned
paths, run the staged publication audit, and keep the index empty after committing.

- [CLI and format details](docs/usage.md)
- [Plugin examples and installation](examples/plugins/README.md)
- [Adding a renderer](docs/adding-a-renderer.md)
- [Release procedure](docs/releasing.md)
- [Security reporting](SECURITY.md)
