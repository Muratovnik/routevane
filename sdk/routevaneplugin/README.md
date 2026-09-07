# Routevane plugin SDK

An external adapter is a separate program the operator installs. It speaks one
versioned protocol over its own standard input and output, with no network
listener. This avoids a separate network authentication surface; it is not an
isolation guarantee against other processes running as the same OS user.

Import this package and nothing else from Routevane.

## Implementing the protocol

Start from the complete [renderer or source examples](../../examples/plugins/README.md).
Their real manifests and catalog files are consumed by the end-to-end tests.
Import this SDK, embed `Unimplemented` for methods your kind does not support,
implement the kind's methods, and call `Serve` on standard input/output.
The public types and method signatures are in [protocol.go](protocol.go) and
[serve.go](serve.go).

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

Follow the [example build/install procedure](../../examples/plugins/README.md).
The executable name and digest depend on the actual build; the remaining
manifest fields must equal `Manifest()`. A renderer also needs a matching
catalog target, and a source needs a list naming its exact type/revision.
With `ROUTEVANE_PLUGINS_DIR` unset, Routevane starts no external plugin host.

## Protocol version

This SDK speaks version 1. The host offers every version it can speak and your
program picks one; a program that can pick none is refused before it is asked to
do any work. A change that an older peer cannot satisfy increments the version
rather than reinterpreting an existing field.

## Worked examples

`examples/plugins/csv-renderer` and `examples/plugins/static-source` in the
Routevane repository are complete, tested plugins. Their tests run in the
repository's own gate, so they stay correct as the protocol evolves.
