---
status: adopted
---

# ADR 0037: Electron desktop and an independently usable Go CLI

Packaging and application updates are extended by [ADR 0038](0038-windows-application-updates.md).
The process ownership and CLI boundaries below remain in force.

## Context

A browser tab does not own the lifetime of a local server. Closing the interface
left work running without a visible control, and starting it again could collide
with its fixed port. Routevane also uses a dense web interface with container
queries and coordinated View Transitions. Its desktop distribution needs control
over the rendering engine version across operating systems.

## Decision

Use Electron with its bundled Chromium for the desktop shell. Retain Nuxt's
generated UI in the existing Go executable and keep that executable separately
usable and distributable as the CLI. Desktop packaging contains that same binary
and the catalog, not a rewritten backend. This replaces the universal single-file
distribution constraint; it does not change domain or planner dependencies.

Use Electron's maintained Packager for a native application directory. The small
shell needs neither a second frontend bundler nor a custom installer. Sharp
rasterizes the existing SVG mark during packaging; it is not a runtime dependency.
Signing, installer format and update hosting must be settled before enabling
automatic application installation. An unsigned local package is not a published
or signed release. Keep the existing CLI release path working independently.

The renderer has a permanent `routevane://app` origin for display preferences.
Electron's protocol handler forwards to Go on a loopback port, with
a random 256-bit credential sent through a parent-owned pipe. The credential is
absent from command arguments, files and renderer APIs. Go requires it for UI/API
requests; subscriptions keep their existing separately authenticated bearer URL.
On first launch the OS assigns a free port; Go commits it under the data lock
before publishing URLs and reuses it on later launches. An occupied saved port
is a startup error, never a reason to silently change subscriptions or stop another
process. Subscription URLs remain local to the computer and work while it is running.

Use a single-instance lock before spawning Go. A second launch foregrounds the
existing window. Closing the window hides it to the tray, preserving its draft;
the tray provides Open and Quit. Quit closes the pipe and cancels owned requests.
Keep the icon in a stopping state while bounded device rollback completes. A
parent crash also closes the pipe. Never kill a process by port or implicitly
attach to another server. A separately started CLI retains its own lifetime;
the existing data lock rejects competing writers.

Source refresh scheduling remains in Go. Desktop application updates belong to
the package manager/updater and must wait for owned work to finish before replacing
files. A future explicit background service may own the Go core without any window;
the current shell does not install one, register startup tasks or enable application
auto-update. CLI users need neither Electron nor its updater.

## Alternatives and consequences

Wails and Tauri offer smaller distributions but normally use system WebViews.
That trades control of engine capabilities and fixes for smaller downloads. A
fixed WebView2 runtime addresses Windows only. Browser-plus-tray still leaves tab
ownership and renderer variation unresolved. The chosen tradeoff is a larger
desktop package and responsibility for shipping Chromium security updates.

Keep Node out of renderers, use isolation, sandboxing and the existing CSP, deny
unexpected permissions/windows/network requests, and confirm external browser
links. No arbitrary IPC or filesystem primitive is exposed to the UI.

## Verification and references

`tools/dev.ps1 check` includes desktop boundary tests. `test-desktop` packages the
real native application, reads health/UI identity, persists a route/preferences,
checks close-to-tray and second launch, then verifies Quit and parent-crash cleanup
and reopening the same data. CI exercises the Windows package; other OS claims
require native acceptance. Existing browser and CLI gates continue to run.

- [Electron security](https://www.electronjs.org/docs/latest/tutorial/security)
- [Electron protocols](https://www.electronjs.org/docs/latest/api/protocol)
- [Electron tray behavior](https://www.electronjs.org/docs/latest/api/tray)
- [Electron Packager options](https://electron.github.io/packager/main/interfaces/Options.html)
- [Wails installation requirements](https://wails.io/docs/gettingstarted/installation/)
- [Tauri WebView versions](https://tauri.app/reference/webview-versions/)
