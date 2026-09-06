# Security policy

## Supported versions

Routevane is at version `0.y.z`. Only the latest published release receives
fixes; there are no maintenance branches for earlier versions. Before reporting,
check whether the problem is still present in the latest release.

## Reporting a vulnerability

Report vulnerabilities privately by email to
[el.muratovnik@gmail.com](mailto:el.muratovnik@gmail.com). Do not open a public
issue for a security problem.

Include the Routevane version, the distribution (desktop application or CLI
package), the affected platform, the impact, and a minimal reproduction with
synthetic data. Do not include live router credentials, subscription tokens,
private network addresses, or unredacted local runtime data.

## What to expect

You receive an acknowledgement by email. A confirmed vulnerability is fixed in
the next release and noted in the [changelog](CHANGELOG.md); reporters are
credited there when they want to be. There is no bug bounty.

## Security boundary

Routevane's security boundary is local but not trusted by default:

- DNS, HTTP feeds, redirects, HAR files, browser traffic, YAML, and device
  responses are untrusted inputs.
- Loopback, link-local, private, multicast, and metadata endpoints are denied
  unless a narrowly documented local workflow explicitly owns them.
- Secrets never enter logs, snapshots, generated artifacts, fixtures, or Git.
- An artifact is published only after validation; a deploy starts only after a
  verified backup and has a tested rollback path.
- A vulnerability or dependency scan is evidence for the current tree, not a
  substitute for boundary tests.
