---
status: adopted
---

# ADR 0007: browser discovery boundary and driver

## Context

Milestone 6 turns one user-supplied URL into a safe local service definition,
and the plan names Playwright as the way to perform the single managed page
load. Two facts make that a technical choice worth re-deciding rather than
copying.

The executable is one Go binary and, since Milestone 4, deliberately has no Node
runtime at product runtime. `playwright-go` is maintained and tracks the same
Playwright version this repository already pins, but it drives the browser
through Playwright's Node driver, which would put a Node process back inside the
product's runtime dependency set.

A page load is also the most hostile input this product accepts. The page
chooses the hosts, the volume, and the number of requests, and it runs on the
operator's own machine beside a loopback API and a bearer-token subscription
store. The boundary matters more than the driver.

## Decision

- The single managed page load is driven by `chromedp`, a maintained pure-Go
  Chrome DevTools Protocol client. No Node process is involved at product
  runtime, and the browser stays one owned external binary, exactly as the web
  gate already treats browsers.
- The browser executable is never discovered from the search path. It comes from
  an explicit flag or the `ROUTEVANE_BROWSER` variable, and a missing or
  non-regular path is refused before a profile is created.
- Every byte the browser sends passes through an in-process HTTP CONNECT proxy.
  The browser is started with `--proxy-server` pointing at it and
  `--proxy-bypass-list=<-loopback>`, which removes the implicit localhost
  exception, so a request the proxy does not open cannot leave the process at
  all. That single chokepoint, not a per-request interception callback, is what
  makes "local network access is denied" checkable.
- The proxy applies the same `netpolicy` destination policy as the feed source:
  a hostname that names this machine, and any answer containing a non-public
  unicast address, is refused. Plaintext requests are refused rather than
  forwarded, because the session starts from an HTTPS URL and forwarding a
  downgraded subresource would widen the boundary for no discovery value.
- Requests, distinct hosts, total bytes, and wall-clock time are bounded, and
  the session is one navigation plus one settle delay. A host counts as
  contacted only after the policy accepted it and a connection existed; a
  refused host is reported as refused and never as evidence about the page.
- The browser's own update, sign-in, and telemetry hosts are refused and
  excluded from the observed set. Flags alone do not suppress them reliably
  across builds. That list describes the browser, not the site, so it never
  affects which of the site's dependencies may be activated.
- The isolated temporary profile is created per session and removed on success,
  failure, and cancellation. The path is reported so a caller can assert that
  nothing was left behind.
- `TrustedSPKI` pins additional server public keys by SHA-256 of their
  SubjectPublicKeyInfo. It trusts exactly the listed keys, so it is a pin rather
  than a way to disable certificate verification.
- The registrable domain comes from `golang.org/x/net/publicsuffix`. A host is
  accepted into a draft because it sits under the entered site's registrable
  domain, and for no other reason. Every other host is a recorded candidate.
  There is no vendor list of CDNs, authentication platforms, or analytics
  endpoints, because the structural rule already covers all of them and a vendor
  list would be a maintenance trap that decides activation.
- A draft carries domains only. An observed address literal stays a candidate, a
  manual seed that parses as an address or has no registrable domain is refused,
  and the catalog writer refuses any non-domain seed kind. Observed addresses
  live only in the observation store.
- A draft is written through the same strict decoder the catalog uses, into a
  temporary file committed by link, and never replaces an existing definition.
  The draft is written before the first DNS cycle runs, so observation uses the
  catalog's own revisions rather than a synthetic identity; a failed first
  observation is reported and leaves the reviewable service in place.
- A browser never starts from an unconfirmed value. The unconfirmed call returns
  exactly the canonical URL the confirmed call will load.

## Learning sessions

Milestone 7 reuses this whole boundary for a guided session. A scenario is an
ordered list of steps, each naming the component its observations belong to, and
the proxy records which step was running when a host was contacted. The step
identity is stored as an `observed_in_session` relation under a reserved
`.step.routevane.invalid` name, so a session attribution is inspectable without a
new table and can never collide with a real host.

Session provenance is stored under the `learning-session` source. Because that is
not a catalog source, its revision never matches an active one, so provenance can
never influence routing on its own. Activation stays structural: the
registrable-domain relationship, the component taxonomy, and whether a step
exercising that component actually ran. Telemetry and advertising are recorded and
never activated, and anything unattributed stays a dependency rather than being
guessed.

A HAR import produces the same evidence shape from a page's component label. An
unlabelled page leaves its hosts unattributed rather than guessing, which is what
keeps the imported and the live path identical in meaning.

## Consequences

Discovery needs a Chromium-family browser the operator supplies. `tools/dev.ps1
test-browser` resolves the exact revision this repository pins through
`playwright-core`, so the Go tests and the Playwright tests never disagree about
which browser was exercised. The default gate stays browserless and the browser
tests skip there.

Refusing plaintext means a site that serves a subresource over HTTP contributes
that host as a refused candidate rather than as evidence. That is visible in the
report rather than silent.

The proxy sees hostnames, not URLs, because a CONNECT tunnel carries only
`host:port`. That is enough for the draft and is less than a URL-level
interception would collect.

Moving to `playwright-go` later would mean accepting a Node driver in the
product runtime and re-implementing the chokepoint on Playwright's routing API;
it requires a new ADR rather than a swap.
