# Routevane plugin SDK

An external adapter is a separate program the operator installs. It speaks one
versioned protocol over its own standard input and output — there is no listener,
no port, and no socket, so nothing else on the machine can reach a plugin or
impersonate the host.

Import this package and nothing else from Routevane.

## A renderer in full

```go
package main

import (
	"fmt"
	"os"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

type handler struct{ plugin.Unimplemented }

func (handler) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:            "my-renderer",
		Version:         "1.0.0",
		ProtocolVersion: plugin.ProtocolVersion,
		Kind:            plugin.KindRenderer,
		Permissions:     []plugin.Permission{plugin.PermissionRenderPlan},
		Renderer: &plugin.RendererManifest{
			ID:                 "my-format",
			FormatVersion:      "my-format-v1",
			ContentType:        "text/plain",
			FileExtension:      "txt",
			SupportedRuleKinds: []string{"ipv4", "prefix4"},
		},
	}
}

func (handler) Render(plan []byte) ([]byte, error)             { /* ... */ }
func (handler) ProjectedRuleCount(plan []byte) (int, error)    { /* ... */ }
func (handler) Validate(payload []byte) error                  { /* ... */ }

func main() {
	if err := plugin.Serve(handler{}); err != nil {
		fmt.Fprintln(os.Stderr, "stopped:", err)
		os.Exit(1)
	}
}
```

`Unimplemented` supplies the methods your kind does not serve; each refuses
rather than pretending to succeed.

## What the host requires

- **The installed manifest is authoritative.** `manifest.json` on disk is what
  the operator reviewed. If what your program reports at handshake differs in
  name, version, kind, protocol version, permissions, or format metadata, the
  host refuses the plugin. The executable name and its checksum are install-time
  facts your program neither knows nor reports.
- **Your validator decides.** After your renderer returns an artifact, the host
  asks your own `Validate` to accept it. A renderer that cannot read back what it
  wrote is refused before anything is published. Make `Validate` an independent
  parse plus a re-render and require byte equality; that is what makes a
  non-canonical file impossible to publish.
- **Determinism.** The same plan must produce the same bytes. Sort and
  deduplicate; never depend on map iteration order, wall-clock time, or a
  process-local counter.
- **A permission belongs to a kind.** A renderer may hold `render_plan`; a source
  may hold `observe_names` and `network`. Asking for anything else is refused at
  install time. Declaring `network` is a disclosure to the operator, never a
  grant of anything the host holds.
- **Nothing of the host's reaches you.** Your process is started with a minimal
  environment, in your own directory, with only its three standard streams. No
  subscription token, no device credential, and no database path is passed to
  you, and the host never sends one over the protocol.
- **The host re-checks every value.** A source's observations are parsed by the
  host before they become sightings, so a malformed address is refused rather
  than stored, however your plugin obtained it.
- **Bounds are the host's.** Every call has a deadline, every frame has a byte
  limit, and a plugin that hangs is terminated. The host launches a private
  checksum-verified snapshot under OS CPU, memory/address-space, descriptor, and
  process-tree limits before any plugin byte can run. A plugin that crashes
  fails its own call and nothing else. These are resource limits, not a
  filesystem or network sandbox for deliberately malicious code.
- **Standard output carries frames only.** Write diagnostics to standard error;
  the host reads them line by line and re-emits them as its own structured
  records.

## Installing

```sh
go build -o my-renderer ./cmd/my-renderer
sha256sum my-renderer                       # the digest goes in manifest.json
mkdir -p "$ROUTEVANE_PLUGINS_DIR/my-renderer"
cp my-renderer manifest.json "$ROUTEVANE_PLUGINS_DIR/my-renderer/"
```

`manifest.json`:

```json
{
  "name": "my-renderer",
  "version": "1.0.0",
  "protocol_version": 1,
  "kind": "renderer",
  "permissions": ["render_plan"],
  "executable": "my-renderer",
  "sha256": "<the digest of the file above>",
  "renderer": {
    "id": "my-format",
    "format_version": "my-format-v1",
    "content_type": "text/plain",
    "file_extension": "txt",
    "supported_rule_kinds": ["ipv4", "prefix4"]
  }
}
```

`ROUTEVANE_PLUGINS_DIR` is the only place the host looks. When it is unset no
plugin host is started at all, and the built-in adapters behave exactly as they
did before plugins existed.

A renderer plugin becomes selectable by adding a catalog target whose `renderer`
is your `id` and whose `profile_key` is your `format_version`. A target may not
claim a capability your `supported_rule_kinds` does not list; such a target is
dropped rather than offered.

A source plugin becomes active when a service catalog entry names the source
`type` and the exact `revision` from its manifest:

```yaml
sources:
  - id: my-source
    type: example-source
    revision: example-source-v1
    component: core
    config:
      names: [api.example.com, edge.example.com]
```

Routevane sends those normalized names to `Observe`, re-parses every returned
address or prefix, and records the result as community evidence. The catalog is
portable and can be inspected without a plugin installed, but `refresh` refuses
a missing implementation or a manifest/catalog revision mismatch. See
`docs/plugin-template/source-manifest.json` and
`examples/plugins/static-source` for complete starting points.

## Protocol version

This SDK speaks version 1. The host offers every version it can speak and your
program picks one; a program that can pick none is refused before it is asked to
do any work. A change that an older peer cannot satisfy increments the version
rather than reinterpreting an existing field.

## Worked examples

`examples/plugins/csv-renderer` and `examples/plugins/static-source` in the
Routevane repository are complete, tested plugins. Their tests run in the
repository's own gate, so they stay correct as the protocol evolves.
