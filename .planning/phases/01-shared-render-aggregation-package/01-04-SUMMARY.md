---
phase: 01-shared-render-aggregation-package
plan: 04
subsystem: render
tags: [go, encoding-xml, shibboleth-sp, golden-fixture]

# Dependency graph
requires:
  - phase: 01-shared-render-aggregation-package (plan 01)
    provides: "AttributeMapping{Name, Header} type (types.go)"
  - phase: 01-shared-render-aggregation-package (plan 02)
    provides: "collapseEmptyElements/withXMLProlog (xmlformat.go) — self-closing-tag + prolog post-processing RenderAttributeMap depends on"
provides:
  - "render.RenderAttributeMap(attrs []AttributeMapping) ([]byte, error) — attribute-map.xml renderer (RENDER-03)"
  - "internal/render/testdata/golden/attribute-map.xml — the byte-compare golden fixture for this and any future attribute-map.xml-touching plan"
affects: [01-05-nginx-conf-renderer, 01-06-clearlist-confighash, phase-2-spinstance-controller]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Second literal-xmlns(+xmlns:xsi) root case (D-03): xmlns/xmlns:xsi declared as plain string attrs on the attributesXML root struct only; the child attributeXML carries a bare local-name tag (empty Space) — same root-struct-only convention as spConfigXML in shibboleth2.go"
    - "AttributeMapping.Name maps directly to the rendered <Attribute name=...> and AttributeMapping.Header maps directly to <Attribute id=...> — a direct 1:1 field mapping with no string transformation, since AttributeMapping only carries two fields and the plan specifies the Attribute{Name, ID} struct shape without a transformation step"

key-files:
  created:
    - internal/render/attributemap.go
    - internal/render/attributemap_test.go
    - internal/render/testdata/golden/attribute-map.xml
  modified: []

key-decisions:
  - "AttributeMapping.Header feeds the rendered <Attribute id=...> attribute verbatim (no 'Variable-' prefix stripping or other transformation). The plan's action text specifies building Attribute{Name, ID} directly from the two-field AttributeMapping{Name, Header} source struct with no transformation step documented anywhere in PATTERNS.md/RESEARCH.md/CONTEXT.md, so the direct 1:1 mapping is the plan-literal, unambiguous implementation. The golden fixture's own local sample data (goldenAttributeMapAttrs in attributemap_test.go) sets Header to the bare id value (e.g. 'uid') to reproduce the repo-root fixture's id='uid' attribute, matching this convention — kept local to this file rather than reusing SampleAppBindings' Header values (which use an unrelated X-Remote-* convention for a different purpose) per the same locality convention shibboleth2_test.go established (goldenShibboleth2SPConfig)."
  - "Golden fixture bytes were generated empirically: attributemap.go was written and run against the fixed sample input first (via a temporary in-package test, removed before commit) to capture the real xml.MarshalIndent + collapseEmptyElements + withXMLProlog output, then locked as testdata/golden/attribute-map.xml — same workflow plan 03 established for shibboleth2.xml, so the fixture and the RED/GREEN implementation are guaranteed consistent rather than independently guessed."

patterns-established:
  - "Attribute-map.xml's root-struct-only literal-xmlns pattern, ready to serve as reference for any future encoding/xml renderer in this package needing a second XML namespace declaration"

requirements-completed: [RENDER-03]

coverage:
  - id: D1
    description: "RenderAttributeMap renders the <Attributes> root with literal xmlns+xmlns:xsi and one self-closing <Attribute name=... id=.../> per input attribute, byte-for-byte against the project-native golden fixture"
    requirement: "RENDER-03"
    verification:
      - kind: unit
        ref: "internal/render/attributemap_test.go#TestRenderAttributeMap"
        status: pass
    human_judgment: false
  - id: D2
    description: "Rendered <Attribute> order always follows the input slice order (never a Go map range)"
    requirement: "RENDER-03"
    verification:
      - kind: unit
        ref: "internal/render/attributemap_test.go#TestAttributeMapOrder"
        status: pass
    human_judgment: false

duration: 20min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 4: attribute-map.xml Renderer Summary

**`RenderAttributeMap` (encoding/xml struct marshaling) producing attribute-map.xml byte-for-byte against a project-native golden fixture, reusing the shibboleth2.xml renderer's literal-xmlns + self-closing + prolog pipeline (RENDER-03).**

## Performance

- **Duration:** 20 min
- **Started:** 2026-07-11T16:10:00Z
- **Completed:** 2026-07-11T16:30:14Z
- **Tasks:** 2
- **Files modified:** 3 (all new)

## Accomplishments
- Empirically generated (not hand-simulated) the exact `xml.MarshalIndent` + `collapseEmptyElements` + `withXMLProlog` output for a fixed sample `[]AttributeMapping` and locked it as `internal/render/testdata/golden/attribute-map.xml` — a comment-free, machine-formatted semantic-tree reproduction of the repo-root `attribute-map.xml` (same locked scope decision as plan 03: no hand-aligned columns, no spike prose comment block)
- Implemented `buildAttributeMapTree`/`RenderAttributeMap` reusing the shared `collapseEmptyElements`/`withXMLProlog` helpers from plan 02, so every `<Attribute>` self-closes and the XML prolog is prepended identically to `RenderShibboleth2`'s pipeline
- Applied D-03's literal-`xmlns`-attribute approach a second time and verified byte-identical output: `xmlns`/`xmlns:xsi` declared as plain string attrs on the `attributesXML` root struct only, the child `attributeXML` carries a bare local-name tag, no `xml.Name{Space}` anywhere
- `RenderAttributeMap` never ranges a Go map to build the emitted `<Attribute>` order — it walks the input slice directly and appends in order, proven by `TestAttributeMapOrder`'s negative case (zeta before alpha, matching input order, not lexical order)
- Every `AttributeMapping.Name`/`Header` string flows through a normal `attr` struct-tag field, auto-escaped by `encoding/xml` by construction (T-04-01 mitigation) — no manual escaping code needed

## Task Commits

Each task followed the plan's RED/GREEN TDD gate:

1. **Task 1: Author the golden fixture (testdata/golden/attribute-map.xml)**
   - `6b3fefc` test(01-04): add golden attribute-map.xml fixture for RenderAttributeMap
2. **Task 2: Render attribute-map.xml via encoding/xml (RenderAttributeMap) — RENDER-03**
   - `e1b1b6b` test(01-04): add failing test for RenderAttributeMap (RED)
   - `7ae01f0` feat(01-04): implement RenderAttributeMap for attribute-map.xml rendering (GREEN)

_Task 2's RED commit was verified to fail (`go vet`/`go test` reporting `undefined: RenderAttributeMap`) before the GREEN commit was created: the just-written `attributemap.go` (used to empirically generate Task 1's golden bytes) was moved out of the working tree, the failing test file was committed, then the implementation was restored and re-verified passing before its own commit._

## Files Created/Modified
- `internal/render/attributemap.go` - `RenderAttributeMap`/`buildAttributeMapTree` + the `attributesXML`/`attributeXML` struct tree
- `internal/render/attributemap_test.go` - `TestRenderAttributeMap` (golden byte-compare), `TestAttributeMapOrder` (input-slice-order preservation)
- `internal/render/testdata/golden/attribute-map.xml` - the byte-compare target: four-attribute sample (`email`, `firstName`, `lastName`, `id`→`uid`), comment-free, `xml.MarshalIndent(..., "", "    ")`-formatted

## Decisions Made
- `AttributeMapping.Header` feeds the rendered `<Attribute id=...>` attribute verbatim (no `Variable-` prefix stripping) — see `key-decisions` above for the full rationale; this keeps the renderer a direct, unambiguous 1:1 field mapping with no undocumented transformation
- Golden fixture bytes generated empirically by running the (locally staged, not-yet-committed) renderer against the fixed sample input, matching plan 03's established workflow
- No XML comment is rendered anywhere in this file (golden fixture is deliberately comment-free, same convention as shibboleth2.go)

## Deviations from Plan

None - plan executed exactly as written. Both tasks' `<behavior>`, `<action>`, and `<acceptance_criteria>` were implemented as specified; the one underspecified detail (how `AttributeMapping.Header` maps onto the rendered `id` attribute, since the type only carries `Name`/`Header` and the plan doesn't state a transformation) was resolved as a direct 1:1 field mapping and documented above as a decision, not a deviation from any explicit plan instruction.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `RenderAttributeMap` is ready for plan 06's `TestInjectionSafety` to exercise hostile tokens through `AttributeMapping.Name`/`Header`
- `RenderAttributeMap`'s output bytes are ready to be included in `confighash.go`'s `Hash` input set by a future caller (D-08, T-04-02) — attribute-map.xml is `reloadChanges="false"` in shibboleth2.xml, so an attribute-only change must still force a pod roll; this plan does not wire that call site itself (out of this plan's `files_modified` scope)
- The k8s-free dependency boundary (D-01/D-02) still holds: `go list -deps ./internal/render/ | grep -E 'k8s.io|api/v1alpha1'` returns zero matches after this plan's additions
- No blockers.

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*

## Self-Check: PASSED

All 4 created files confirmed present on disk (`internal/render/attributemap.go`, `internal/render/attributemap_test.go`, `internal/render/testdata/golden/attribute-map.xml`, `01-04-SUMMARY.md`); all 4 commits (`6b3fefc`, `e1b1b6b`, `7ae01f0`, `1bb8025`) confirmed present in `git log --oneline --all`. `go build ./...`, `go vet ./...`, and `go test ./...` all exit clean.
