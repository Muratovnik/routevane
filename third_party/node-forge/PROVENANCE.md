# node-forge security derivative

This private package retains the runtime of the official node-forge 1.4.0
tarball and applies only the nested DigestAlgorithm validation hunk from
[upstream PR 1152](https://github.com/digitalbazaar/forge/pull/1152).
[PROVENANCE.json](PROVENANCE.json) records its exact source, tarball integrity,
patch revision, human patch author and runtime hashes. The runtime diff is
[rsa-digestalgorithm.patch](rsa-digestalgorithm.patch).

Routevane maintains this derivative until it can adopt a compatible supported
upstream fixed release. The package name and version identify the derivative;
they do not claim a published upstream fix. All other runtime modules remain
unchanged. The upstream [LICENSE](LICENSE) and embedded copyright notices are
retained; Routevane uses the BSD-3-Clause license option.

The public regression vector in `test/upstream-digestalgorithm.json` is copied
from the upstream RSA suite at the recorded patch revision under the same
license. It contains no private key. Tests generate their positive-control keys
in memory and do not save them. Run the standalone checks with
`node --test third_party/node-forge/test/security.cjs` from the repository root.
The web security command also verifies the installed dependency graph and
continues monitoring advisories for the original node-forge identity.
