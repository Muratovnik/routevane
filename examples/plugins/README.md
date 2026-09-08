# Plugin examples

Audience: contributors testing or extending a plugin. These are complete working
programs with matching manifests, not generic placeholders:

| Example | Files used by the end-to-end tests |
| --- | --- |
| [CSV renderer](csv-renderer/main.go) | [manifest](csv-renderer/manifest.json), [target](csv-renderer/target.yaml) |
| [Static source](static-source/main.go) | [manifest](static-source/manifest.json), [list](static-source/list.yaml) |

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
your chosen catalog. For the source, add its list YAML to `builtin/`.
Use a copy of the catalog and a separate data directory for experiments; do not
overwrite the production catalog or database. The source example answers with
synthetic documentation addresses and is not useful as a live routing feed.

A complete test of both supplied files and real subprocesses is:

```powershell
go test ./cmd/routevane -run 'TestPublishesThroughAnExternalRenderer|TestAnInstalledExternalSource|TestPluginsAreDiscovered' -count=1
```

The tests copy these exact manifests, target and list, filling only the binary
name and digest. They exercise handshake, refresh, rendering and checksum refusal.

## Write your own plugin

Follow the [SDK contract](../../sdk/routevaneplugin/README.md). For a plugin that
must build independently of a Routevane checkout, create a fresh Go module and
depend on the published `v0.1.0` SDK. From the Routevane repository root, this
block copies one exact shipped example and builds it without a workspace or
`replace` directive:

```powershell
$Example = 'static-source'
$External = Join-Path $PWD "tmp/published-sdk-$Example"
New-Item -ItemType Directory -Path $External -ErrorAction Stop | Out-Null
Copy-Item "./examples/plugins/$Example/main.go" $External -ErrorAction Stop
Push-Location $External
try {
  go mod init "example.com/$Example"
  if ($LASTEXITCODE -ne 0) { throw 'Module initialization failed' }
  go get github.com/Muratovnik/routevane/sdk/routevaneplugin@v0.1.0
  if ($LASTEXITCODE -ne 0) { throw 'Published SDK download failed' }
  go build -o plugin .
  if ($LASTEXITCODE -ne 0) { throw 'External plugin build failed' }
} finally {
  Pop-Location
}
```

Use `csv-renderer` as `$Example` to check the renderer in the same way. A plugin
developed against unreleased SDK changes can instead use the current checkout:

```powershell
$RoutevaneCheckout = (Resolve-Path 'C:/path/to/Routevane').Path
go mod edit -require=github.com/Muratovnik/routevane@v0.0.0
go mod edit -replace=github.com/Muratovnik/routevane=$RoutevaneCheckout
go mod tidy
go build ./...
```

That replacement is a development path, not a claim that the checkout is a
published SDK. Protocol version 1 still sends the list identity under the JSON
key `service_id`; Go field names in a particular SDK release do not change that
separately versioned wire contract. There is no fictitious published `v0.0.0`
dependency.

Unset `ROUTEVANE_PLUGINS_DIR` to run without plugins. Delete only the disposable
example directory when finished. Installed plugins execute with your OS account;
their resource limits are not a malicious-code filesystem or network sandbox.
