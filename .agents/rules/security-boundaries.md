# Security boundaries

Routed from `AGENTS.md`. Read this before implementing a network, browser,
device, subprocess, file-import, or plugin boundary.

- Treat DNS, HTTP, redirects, YAML, HAR, browser events, device replies, and
  imported artifacts as hostile inputs with explicit size and time limits.
- Resolve and validate every redirect hop. Reject loopback, link-local,
  private, multicast, metadata, and other special-use destinations unless a
  narrowly named local operation explicitly permits one.
- Browser discovery uses an isolated temporary profile, denies local-network
  access, exposes the URL before launch, and cleans up on success, failure, and
  cancellation.
- The discovery proxy (`internal/discovery/proxy.go`) is a destination
  checkpoint, not a client-authentication gate: it binds to loopback and lives
  only for one session's duration, but it does not verify that the browser is
  the process on the other end of a given CONNECT. A local process running as
  the same OS user could dial through it during that window and have its own
  traffic attributed to the session's evidence. This is an accepted boundary
  of the current threat model — local processes under the same OS user are
  trusted relative to a discovery session — not an oversight to silently widen
  later. Authenticating the proxy's client (for example, requiring a
  per-session `Proxy-Authorization` secret and handling `Fetch.authRequired` in
  chromedp) is deliberately out of scope until a concrete threat crosses this
  boundary, because it would move the browser launch from declarative
  chromedp actions to CDP-level request interception, and a half-handled
  `Fetch.enable` can stall every request in the session.
- Do not send network data through a shell. Use typed APIs and explicit
  encoding/decoding policies.
- A DNS address is evidence about an observation, never proof of a suffix, ASN,
  RDAP owner, or broad network. Every accepted or rejected rule carries
  provenance and a stable reason code.
- An observed value becomes a route only if it is a destination. The planner
  refuses a special-use address and a prefix wide enough to swallow an address
  space, whatever declared it, with a reason code. Provenance never buys an
  exemption: loopback declared by an official source is still loopback. See
  ADR 0019.
- Validate renderer output before publication. Keep the previous valid artifact
  available when a candidate fails.
- Device deployment requires version probe, backup, bounded deploy, verify, and
  automatic rollback in the same vertical slice. Logs redact credentials. The
  rollback runs on a context detached from the caller's, with its own budget:
  the cancellation that failed a deployment must not disarm the recovery from
  it. See ADR 0020.
- External plugins wait for their milestone and run out of process with a
  versioned protocol, permissions, timeout, resource limits, and checksums.
