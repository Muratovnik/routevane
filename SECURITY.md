# Security policy

Report vulnerabilities privately by email to
[el.muratovnik@gmail.com](mailto:el.muratovnik@gmail.com). Include the Routevane
version, affected platform, impact, and a minimal reproduction with synthetic
data. Do not include live
router credentials, tokens, private network addresses, or unredacted local
runtime data in a public issue.

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
