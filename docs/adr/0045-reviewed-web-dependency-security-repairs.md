---
status: adopted
---

# ADR 0045: reviewed security repairs for web dependencies

## Context

Nuxt's Nitro 2 integration requires listhen, which directly uses node-forge's
certificate APIs. Boundaries, Stylelint and Nitro also require micromatch's
braces APIs. Current compatible consumer releases retain these dependencies.
Their upstream releases contain
[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
and [GHSA-86w9-cpqp-85rv](https://github.com/advisories/GHSA-86w9-cpqp-85rv),
respectively. Older versions do not escape the affected ranges.

Removing architecture or stylesheet checks would lose required protection.
Substituting picomatch for micromatch or srvx for listhen does not preserve their
APIs. A Nitro 3 beta migration would change Nuxt's server integration and leave
the independent lint dependency paths unresolved.

## Decision

Keep the existing consumers and the full npm audit of development, optional and
peer dependencies at High severity. Use two explicitly identified repairs:

- Override braces with the published MIT
  `@dieub/braces-depth-guard@3.0.3-pn.3`. Its immutable lockfile integrity pins
  the guard-only derivative of braces 3.0.3. It bounds string parsing and
  direct AST recursion to 100 levels, including caller-supplied cyclic children.
  It preserves the callable CommonJS API, compilation and expansion behavior
  below that bound. This limit addresses stack exhaustion; it does not add a
  general expansion-cardinality or AST-width budget.
- Override node-forge with the local private package
  `@routevane/node-forge@1.4.0-routevane.1`, under
  [third_party/node-forge](../../third_party/node-forge/PROVENANCE.json).
  Its runtime is the official 1.4.0 tarball with only the nested DigestAlgorithm
  child-count check from upstream
  [PR 1152](https://github.com/digitalbazaar/forge/pull/1152), revision
  `ceba34402e329f0365134f23fe19898756527d65`. The derivative preserves all runtime
  modules and upstream license notices. Routevane uses the BSD-3-Clause option
  of the upstream dual license. It is an owned security repair, not an upstream
  fixed release.

The web package uses npm's `install-links=true` project setting to pack the
local file dependency as an ordinary installed package. The direct dependency
and `$node-forge` override reference the same source. Clean installation must
resolve the repaired bytes from listhen without a dangling nested link.

The source provenance records upstream and repaired runtime hashes. The web
security command verifies that inventory and installed consumer identities,
runs the security and compatibility regressions, and runs the existing full
npm audit. It additionally queries the same npm advisory service under the
original `braces@3.0.3` and `node-forge@1.4.0` identities. Only the two repaired
advisories with their reviewed URL, title, severity and affected range are
recognized as fixed. Another High/Critical advisory, or a change to one of
those fields, blocks the command. Lower severities remain visible and registry
errors fail closed. Changing a package name alone never establishes remediation.

## Verification and maintenance

The regression controls must fail against the unmodified dependencies and pass
against the repairs. Forge's unchanged upstream public test vector must be
rejected through the default signature-verification API; valid signatures and
certificate/PKCS12 operations must still work. Brace tests cover string and AST
depth, cyclic children, escaped patterns, ranges and real micromatch APIs.
Architecture allow/deny behavior, stylesheet discovery, Nuxt generation and dev
server lifecycle remain covered by the existing mandatory checks.

These repairs are maintained with the product. Any source, package identity,
upstream advisory or patch change requires review and the affected regression
and integration checks. Monitor upstream releases when updating web
dependencies. Replace each derivative with a compatible supported fixed release
when it preserves the same behavior and passes the full gates; remove its repair
recognition and local source together. An unrelated package update does not
justify enlarging the recognized advisory set.

Restoring the prior manifest and lockfile, then reinstalling them, rolls back
the dependency change. It also restores the High findings, so that state cannot
pass the security gate or serve as an accepted release candidate.
