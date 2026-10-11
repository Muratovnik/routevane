---
status: adopted
---

# ADR 0048: Choose new desktop ports below the ephemeral ranges

## Context

The desktop persists its loopback port so subscription addresses survive a
restart (ADR 0037). A first launch, and an explicit recovery (ADR 0047), bound
port 0 and kept whatever the OS assigned. The OS assigns from its dynamic
(ephemeral) range: 49152–65535 by default on Windows, 32768–60999 on Linux.
Windows also places Hyper-V and WinNAT port exclusions in that range and uses
it for outgoing connections. On one Windows workstation,
`netsh int ipv4 show excludedportrange protocol=tcp` listed 21 hundred-port
blocks between 49756 and 64025 and only single excluded ports below 49152. The
saved desktop port there had fallen inside one of those blocks, so it could no
longer bind although nothing listened on it.

## Decision

In desktop mode only, choose a new port uniformly at random from 20000–29999.
The band is below the default ephemeral ranges of Windows and Linux and below
the Kubernetes NodePort range 30000–32767, which local clusters bind on
localhost. The macOS default range is also high, but it was not checked
against a primary source for this decision.

Bind `127.0.0.1` through the existing listener seam. On any bind error, try
another untried band port, at most 32 attempts. Recovery never tries the saved
port that just failed. If every attempt fails, bind port 0 as before and log
the reason code `desktop_port_band_unavailable` without raw diagnostics. The
random choice uses `crypto/rand` and is injectable for tests.

The chosen port is persisted exactly as before: a first launch saves it before
any URL is published, and recovery replaces it after composition (ADR 0047).
Ordinary starts reuse a saved port as-is, including one outside the band,
because copied subscription addresses carry it. Independent `serve` keeps its
`--port` behavior.

Rejected alternatives:

- Port 0 alone keeps landing in the range where Windows reserves blocks.
- A fixed default port with a scan collides with other installations and
  loses a predictable address as soon as the scan moves.
- A port derived from a hash of the data directory lands on the same blocked
  port every time.
- Creating `netsh` exclusions needs elevation and changes system state, which
  ADR 0047 forbids.

## Consequences and verification

New desktops and recoveries get ports that a default Windows or Linux
configuration does not reserve or hand to outgoing connections. Existing saved
ports are kept, so an installation affected before this decision still needs
one explicit recovery. A machine whose dynamic range was moved down, for
example to start at 1025, can still reserve inside the band. Other services
can also listen there. Bind-and-retry covers a conflict at selection, and
ADR 0047 recovery covers a later one.

Sources: [default dynamic port range for TCP/IP](https://learn.microsoft.com/en-us/troubleshoot/windows-server/networking/default-dynamic-port-range-tcpip-chang)
and [`ip_local_port_range`](https://www.kernel.org/doc/html/latest/networking/ip-sysctl.html).

`cmd/routevane/desktop_test.go` covers a first launch that announces and
persists a band port, retries over refused band ports that never repeat, the
port 0 fallback with its reason code after 32 refusals, recovery that never
retries the failed saved port and persists a band port, and reuse of a saved
port outside the band. The band search is checked with constant and
cryptographic sources.
