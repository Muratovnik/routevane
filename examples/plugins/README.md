# Plugin examples

Audience: contributors testing or extending a plugin. These are complete working
programs with matching manifests, not generic placeholders:

| Example | Files used by the end-to-end tests |
| --- | --- |
| [CSV renderer](csv-renderer/main.go) | [manifest](csv-renderer/manifest.json), [target](csv-renderer/target.yaml) |
| [Static source](static-source/main.go) | [manifest](static-source/manifest.json), [service](static-source/service.yaml) |

The zero digest in each manifest is an explicit build-time placeholder. Only
`executable` and `sha256` must be filled for the binary you built; the other
metadata must match the program's handshake.

## Build and install into a disposable directory

Complete [developer setup](../../CONTRIBUTING.md) first. From the repository root,
run this PowerShell block for each example you want (`csv-renderer` or
`static-source`):

```powershell
$Example = 'csv-renderer'
$Plugins = Join-Path $PWD '.cache/example-plugins'
$Destination = Join-Path $Plugins $Example
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
$BinaryName = if ($IsWindows) { 'plugin.exe' } else { 'plugin' }
go build -o (Join-Path $Destination $BinaryName) "./examples/plugins/$Example"
if ($LASTEXITCODE -ne 0) { throw 'Plugin build failed' }
$Manifest = Get-Content "./examples/plugins/$Example/manifest.json" -Raw | ConvertFrom-Json
$Manifest.executable = $BinaryName
$Manifest.sha256 = (Get-FileHash (Join-Path $Destination $BinaryName) -Algorithm SHA256).Hash.ToLowerInvariant()
$Manifest | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $Destination 'manifest.json') -Encoding utf8NoBOM
$env:ROUTEVANE_PLUGINS_DIR = $Plugins
```

For the renderer, add the supplied target YAML to the `targets/` directory of
your chosen catalog. For the source, add its service YAML to `builtin/`.
Use a copy of the catalog and a separate data directory for experiments; do not
overwrite the production catalog or database. The source example answers with
synthetic documentation addresses and is not useful as a live routing feed.

A complete test of both supplied files and real subprocesses is:

```powershell
go test ./cmd/routevane -run 'TestPublishesThroughAnExternalRenderer|TestAnInstalledExternalSource|TestPluginsAreDiscovered' -count=1
```

The tests copy these exact manifests, target and service, filling only the binary
name and digest. They exercise handshake, refresh, rendering and checksum refusal.

## Write your own plugin

Follow the [SDK contract](../../sdk/routevaneplugin/README.md). Create your own Go
module and depend on an actually published Routevane tag, for example
`go get github.com/Muratovnik/routevane/sdk/routevaneplugin@v0.1.0` after that
release exists. Before the first publication, a local `go mod edit -replace`
pointing at your Routevane checkout is a development-only alternative.
There is no fictitious `v0.0.0` template dependency.

Unset `ROUTEVANE_PLUGINS_DIR` to run without plugins. Delete only the disposable
example directory when finished. Installed plugins execute with your OS account;
their resource limits are not a malicious-code filesystem or network sandbox.
