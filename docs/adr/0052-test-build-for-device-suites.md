---
status: adopted
---

# ADR 0052: a test build for the device and credential suites

## Context

The browser suites drive the built binary, but two of its boundaries kept the
device flows out of their reach.

- The device policy (ADR 0019, `internal/netpolicy/device.go`) accepts only
  private and link-local addresses. A suite therefore had to name an address
  that could be a real router on the workstation's network, or stub the
  registry routes in the page.
- A saved connection password (ADR 0050) goes to the operating system's
  credential store. A suite must never write to the workstation's own store, so
  saving a password was proved only against stubbed routes and Go tests with a
  fake store.

The protected outcome is unchanged: a release binary accepts the same device
addresses, keeps passwords in the same store and contacts devices exactly as
before. A test-only widening must be impossible to switch on in a shipped
binary.

## Decision

- **A Go build tag, `routevane_testmode`, compiles the test build.** It changes
  three things and nothing else:
  - The device policy also accepts the RFC 5737 documentation ranges
    `192.0.2.0/24`, `198.51.100.0/24` and `203.0.113.0/24`. Every other
    decision of `DeviceDestination` is unchanged: loopback, the metadata
    address, carrier-grade NAT and the other special-use ranges stay refused.
  - The credential store is `secretstore.Memory`, held in the process and gone
    when it exits. It keeps the store's contract, including `ErrNotFound` for
    an absent key, and never puts a secret into an error.
  - The device dialer refuses every connection, so the test build sends no
    packet to a device address, documentation range or not. A manual send gets
    as far as the device probe and fails there.
- **The release build cannot contain it.** The tagged files are compiled only
  with the tag; without it, `testnet_release.go` and `testmode_release.go`
  supply a policy with no additions, the operating system store and the
  standard dialer. There is no environment variable, flag or setting that turns
  any of this on at run time.
- **The test build announces itself.** `routevane version` ends with
  `(test build)` and `serve` logs `build=test` once at start. Go also records
  the tag in the binary's build settings, which `go version -m` reads.
- **The test build lives at `.cache/test-build/`.** `tools/dev.ps1 build` and
  `test-browser` write it there after the usual core in `.cache/build/`, which
  the desktop shell and its packager read. The browser suites drive only the
  test build and refuse a binary that does not report `(test build)`.

| Command or job | Core it builds | Tag |
| --- | --- | --- |
| `tools/dev.ps1 build`, `test-browser` | `.cache/build/` and `.cache/test-build/` | none / `routevane_testmode` |
| `up`, `desktop`, `package-desktop`, `installer`, `test-update`, `test-desktop` | `.cache/build/` or the update fixtures | none |
| `release` | one binary per platform | none |
| `tools/dev_server.py` (`dev`) | its own build | none |
| `check` (Go gate) | `.cache/build/` | none |

`Invoke-CoreBuild` checks every core it writes against `go version -m`: a core
that must be release-shaped is refused if it carries the tag, and the test build
is refused if it does not. `release` checks each platform binary the same way
before it is archived.

- **Tests are split by build.** Files ending `_release_test.go` run in the
  ordinary untagged gate and prove that a documentation address is refused and
  that the operating system store and standard dialer are composed. Files
  ending `_testmode_test.go` (and `testmode_test.go` in `cmd/routevane`)
  compile only with the tag; `tools/dev.ps1 check` vets and analyses the tagged
  packages and runs their `TestTestMode` tests.
- **What the browser suites prove.** They run against the test build, not the
  release binary. They establish the interface, the HTTP API and the
  application on top of the test build's policy, store and dialer. They do not
  establish release device policy, Credential Manager behaviour or any device
  contact; the Go suite and physical-device acceptance remain the evidence for
  those.
- **The connection form is not mode-aware.** The page holds the release device
  rule in every build. A suite that needs a documentation-range connection
  registers it through the service.

## Consequences

- A suite can register, save a password for, preview and attempt a send to a
  connection at `192.0.2.10` on the real binary with no stubbed routes and no
  device anywhere on the network.
- The fake secret store in `internal/application` tests is not reused: it lives
  in a `_test.go` file of another package, has no lock for concurrent HTTP
  handlers and answers `application.ErrNotFound` rather than the store's
  `ErrNotFound`.
- The test build cannot prove that a real device answers. Apply results in the
  browser suites are refusals at the probe step by construction.
- Two cores exist after `build`. They differ by tag, and each reports which one
  it is.

## Alternatives

- **A run-time switch** (flag or environment variable) would put the widened
  policy and the in-memory store into every shipped binary, guarded only by a
  value an operator or a stray environment could set, and would need a new
  release gate to prove it stays off. The build tag removes the code instead.
- **Link-local addresses for suites** (`169.254.x.x`) would still be accepted by
  the release policy and could still reach a device on the local link.
