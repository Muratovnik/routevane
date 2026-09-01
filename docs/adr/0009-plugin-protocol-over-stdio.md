---
status: adopted
---

# ADR 0009: plugin protocol over stdio instead of gRPC

## Context

Milestone 10 opens the product to adapters the operator installs and Routevane
does not ship. Its preconditions were met before it started: three built-in
renderers, two sources, one deployer, and the interface changes their
development actually forced.

The plan names the transport as `protobuf contracts` and `versioned gRPC`. That
choice was made before the built-in adapters existed. Two facts about the
delivered product now argue against it.

The first is what the calls are. A renderer is asked for bytes given a plan; a
source is asked for values given names. Every call is one request and one
response, none stream, and the payload each side exchanges is already the
product's own JSON — the same encoding the published artifact, the plan
snapshot, and the local catalog use. Protobuf would introduce a second schema
language for data that has exactly one authoritative shape in Go, and every
plugin author would have to keep the two in step.

The second is what gRPC costs at the boundary. gRPC needs a listener. On the
operator's own machine that means either a loopback port or a named pipe — in
both cases a name any other local process can connect to, and a surface the host
must then authenticate and authorize. It also needs `protoc` and its Go plugins
installed to build, on every developer machine and in the gate, none of which is
part of the Go toolchain this product deliberately restricts itself to
(ADR 0002).

`plugin.Open` was never a candidate. It requires an exact toolchain and
dependency match, gives the plugin the host's own address space, and offers no
place to put a permission or a timeout.

## Decision

The plugin protocol is length-prefixed JSON frames over the child process's own
standard input and output, versioned explicitly, defined in one public package
at `sdk/routevaneplugin`.

- **No listener.** A plugin is reachable only through the pipes its parent
  created. Nothing else on the machine can connect to it or impersonate the
  host, so there is no port, no socket path, no local authentication problem,
  and no accidental exposure to another user on a shared machine.
- **`ProtocolVersion` is a number in the handshake.** The host offers the
  versions it can speak; the plugin picks one; a plugin that can pick none is
  refused before it is asked to do any work. A change an older peer cannot
  satisfy increments the version rather than reinterpreting a field.
- **The installed manifest is authoritative, not what the process says.**
  `manifest.json` is what the operator reviewed. Name, version, kind, protocol
  version, permissions, and format metadata reported at handshake must equal it,
  and the executable's SHA-256 is verified while Routevane copies it into a
  private execution snapshot. The snapshot, not the plugin-owned path, is run,
  so replacement after discovery or during startup is refused rather than run.
- **Permissions belong to a kind and grant nothing of the host's.** A renderer
  may hold `render_plan`; a source may hold `observe_names` and `network`.
  `network` is a disclosure to the operator, not a capability the host hands
  over. No subscription token, device credential, or database path is ever
  passed to a plugin, by environment or by frame: the child starts in its own
  directory with a minimal environment and three streams.
- **Every value crosses back through the host's own checks.** A source's
  observations are re-parsed by the host before they become sightings; a
  renderer's artifact must satisfy the plugin's own validator and then the same
  publication path, size bound, and semantic hash as a built-in format. A plugin
  cannot introduce an unparsed address or a non-canonical artifact.
- **Bounds are the host's.** Frames are capped, every call has a deadline, a
  hung plugin is terminated, and a crashed plugin fails its own call only. A
  private runner waits until containment is installed before it executes plugin
  bytes. On Windows a Job Object caps the process tree, memory, and CPU time and
  kills the tree when closed. On Linux and macOS the runner applies address
  space, CPU, open-file, and core-dump limits before `exec`, and the host owns a
  separate process group it can terminate. The plugin's standard error is read
  line by line and re-emitted as the host's own structured records.
- **A plugin never replaces a built-in.** Claiming an existing renderer id or
  source type is refused at startup, because the built-in is what the product's
  own tests cover.
- **With no plugin directory, no plugin host is started.** `ROUTEVANE_PLUGINS_DIR`
  is the only place the host looks; unset, the binary behaves exactly as it did
  before plugins existed.

## Consequences

Every criterion the milestone states is met and covered by tests, including
deliberately crashing and hanging plugins in `internal/plugin/testdata`.

The product keeps one binary, one dependency set, and one build command. A
plugin author needs the Go toolchain and this repository's SDK package — no code
generator, no global tool install, and no schema kept in step by hand.

What is given up is a language-agnostic contract for free. A plugin in another
language must implement the framing itself; the format is small and documented,
but it is not a `.proto` a generator will produce. Streaming is also out of
scope: a future call that genuinely needs it would increment the protocol
version rather than widen an existing message.

Resource containment is not a filesystem or network sandbox. Plugins are code
the operator deliberately installs; the permission manifest discloses intended
capabilities and prevents access to Routevane's own secrets, while OS limits
bound accidental or faulty resource use. Treating an actively malicious plugin
as safe would require platform sandboxes and a different trust decision.

`sdk/routevaneplugin` is now public API. A breaking change there is a protocol
version increment, not an edit.
