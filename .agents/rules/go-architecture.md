# Go vertical-slice architecture

Routed from `AGENTS.md`. Read this before changing Go product code.

- `cmd/routing-agent` is a thin executable boundary. Product decisions live in
  `internal/` packages created only when a slice uses them.
- Dependency direction is domain -> planner -> application -> infrastructure
  and surfaces. An arrow means the right side may depend on the left side.
- Domain values use `net/netip`; parse at the boundary and keep canonical forms
  inside. Never model IP/CIDR as unchecked strings after validation.
- Planner transformations are deterministic pure functions where possible.
  Stable ordering and semantic hashes exclude time, database IDs, and map
  iteration order.
- Declare an interface in the consuming package after the first concrete need.
  Generalize only after a second real implementation proves the seam.
- Wrap errors with operation and stable identity, not secrets or raw payloads.
  Use `errors.Is`/`errors.As` for machine decisions.
- Pass `context.Context` across I/O boundaries. Bound time, bytes, redirects,
  concurrency, retries, and subprocess lifetime at the boundary that owns them.
- Tests mirror user-visible slices. Prefer table tests for transformations,
  fuzz tests for parsers and hostile inputs, property tests for set/hash
  invariants, and golden files only for renderer output.
- Keep CGO optional until SQLite introduces a measured driver choice. A local
  race gate must not silently require a compiler the documented setup omits;
  CI owns the blocking race run on Linux.
