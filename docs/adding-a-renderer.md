---
status: adopted
---

# Adding a renderer

A renderer owns one output format and nothing else. Target limits come from the
target profile, policy decisions come from the planner, and storage and
transport come from the layers that already exist. If a step below asks you to
teach a renderer about services, persistence, or devices, the change belongs
somewhere else.

## 1. Write the format package

Create `internal/renderers/<name>/`. Implement, in this order:

1. `ID`, `Version`, `ContentType`, `FileExtension`, and the format's own byte and
   entry bounds as constants. `ID` and `Version` must satisfy
   `domain.ValidateSlug`; `Version` is the value a target profile declares as its
   `format_key`. `FileExtension` is a bare lowercase suffix with no dot.
2. `Render(domain.RoutingPlan) ([]byte, error)` — project already-decided rules.
   A renderer never adds, drops, or reorders a policy decision; it may only
   deduplicate values the format cannot express twice.
3. `Parse`/`Validate` — decode the bytes **independently of Render**, then render
   the decoded projection again and require byte equality. That equality check is
   what makes non-canonical, padded, reordered, and duplicated documents fail.
4. `ProjectedRuleCount` — the number a target rule limit is compared against. It
   must count canonical entries, so a duplicated plan rule cannot change it.
5. The `Renderer` methods: `ID`, `Version`, `Descriptor`, `SupportedRuleKinds`,
   `ProjectedRuleCount`, `Render`, `Validate`. `Descriptor` returns
   `domain.RendererDescriptor` and must not drift from `ID`/`Version`; the
   registry refuses a registration where it does.

## 2. Register it

Add the renderer to `builtinRenderers()` in `cmd/routevane/plugins.go`. That
function is the only place a renderer id is bound to a built-in implementation,
so no request handler needs a per-format branch. An installed plugin renderer
joins the same registry there and is judged by the same rules; it may not claim
an id a built-in already holds.

## 3. Describe the target

Add `catalog/targets/<target>.yaml` with `format_key` equal to the renderer's
`Version`, `renderer` equal to its `ID`, and constraints that describe the real
device or client. A target may not claim a capability the renderer cannot
express: `renderableTarget` drops such a target instead of letting it be
selected. Add a `frozenProfiles` entry pinning the device-specific truth a
catalog file must not widen.

Several targets may share one renderer. The target catalog and the renderer
registry are separate maps for exactly that reason.

## 4. Test it

- A golden file for the canonical document, regenerated with
  `ROUTEVANE_UPDATE_GOLDEN=1`. Golden files are for formats only.
- A table of hostile and non-canonical documents that `Validate` must reject.
- A fuzz target asserting `Parse` never accepts a document its own projection
  would render differently.
- An order-independence test: permuting `plan.Rules` must not change the bytes.
- An assertion that the supported rule kinds differ from the other formats where
  they should, so a second format is a real second case rather than a copy.
- If the format is a dynamic set — the device resolves names itself — assert the
  artifact carries no observed address. `internal/renderers/openwrtnftset` is the
  worked example: it supports suffix matching only, because the underlying option
  matches every name under a domain and would silently widen an exact rule.

## 5. Verify

```powershell
pwsh -NoLogo -NoProfile -File tools/dev.ps1 check
```

Then publish the same profile through both the new target and an existing one
and confirm each artifact satisfies its own validator and neither satisfies the
other. `cmd/routevane/every_format_e2e_test.go` is the worked example.
