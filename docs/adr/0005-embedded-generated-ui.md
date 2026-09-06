---
status: adopted
---

# ADR 0005: embedded generated UI under a self-only CSP

## Context

Milestone 4 must close the primary scenario without a CLI, but the executable
remains one binary. Serving the Nuxt control surface from Nitro, a dev server,
or any second listener would add a Node runtime, a second port, a proxy, and a
CORS boundary to a product that is defined as loopback-only and single-process.

Nuxt's generated `index.html` carries an inline import map, an inline
`window.__NUXT__` bootstrap script, and inline JSON payload blocks. Serving
those unchanged requires `unsafe-inline` or a per-response nonce/hash pipeline
inside the Go boundary, which widens the attack surface of the one process that
also serves bearer-token subscription bytes.

Generated output is a build product. Committing it would make review diffs
meaningless and would let a stale committed bundle silently outlive the Go
handlers it is served by.

## Decision

- Nuxt is client-only (`ssr: false`) and produces static output. The complete
  generated public tree is synchronized into
  `internal/infrastructure/httpapi/ui/` and embedded with `go:embed all:ui`.
  The same `serve` process serves the UI and `/v1`; there is no Node, Nitro,
  proxy, CORS layer, or external asset origin at product runtime.
- The generated directory is ignored except for a tracked
  `routevane-ui.marker`, so a clean checkout compiles without generated output.
  `tools/dev.ps1 build`, `check`, and `test-browser` generate and synchronize it
  immediately before the Go build; the sync refuses an unexpected target, a
  missing or altered marker, a reparse point on either side, and an empty
  public tree.
- Before synchronization the tool rewrites the generated HTML into a form that
  needs no inline execution: the import map is removed and its `#entry`
  specifier is substituted directly into the emitted `/_nuxt/` scripts, the
  bootstrap script is written as a content-addressed
  `/_routevane/nuxt-bootstrap.<sha256>.js` asset, and `application/json`
  payload blocks become hidden `div` elements. The tool fails the build if any
  inline script, inline style, residual `#entry`, or absolute URL survives.
- Every UI response carries
  `default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self';
  img-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none';
  frame-ancestors 'none'; form-action 'none'`. No `unsafe-*`, wildcard, or
  external source is used.
- Root and SPA deep links return the embedded document as `no-store`; hashed
  `/_nuxt/` assets are `immutable`; other files are `no-cache`; the marker and
  unknown paths remain 404. `/health` and `/v1` keep API semantics. The embed
  boundary hashes the sorted per-file digests and publishes the set identity as
  `X-Routevane-UI-Digest`, which the browser acceptance test asserts against an
  independently computed digest before it trusts the listener.
- The build response is a bounded safe projection: raw `RoutingPlan` bytes are
  removed from snapshot serialization, and routing evidence stays reachable only
  through the explicit snapshot diagnostics read.

## Consequences

The canonical tool command is now the only way to produce a UI-bearing binary;
a bare `go run ./cmd/routevane serve` compiles and serves the API but has no
UI assets. Reproducibility of two consecutive builds is measured rather than
assumed, because Nuxt's generated build metadata can carry a fresh identifier.

The HTML rewrite is coupled to Nuxt's generated output shape. A Nuxt upgrade
that changes the import map, bootstrap script, or payload markup fails the build
loudly instead of silently degrading the CSP. That failure is the intended
signal; relaxing the CSP is not an acceptable response to it.

Reverting to a separately served UI would reintroduce a second runtime,
listener, and origin boundary, so it requires a new ADR rather than a
configuration switch.
