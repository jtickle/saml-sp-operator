---
phase: 01-shared-render-aggregation-package
plan: 02
subsystem: render
tags: [go, encoding-xml, sha256, regexp]

# Dependency graph
requires:
  - phase: 01-shared-render-aggregation-package (plan 01)
    provides: "internal/render package skeleton; ConfigFile type already declared in types.go"
provides:
  - "collapseEmptyElements(b []byte) []byte — self-closing-tag post-process for encoding/xml output (RESEARCH Pattern 1)"
  - "withXMLProlog(body []byte) []byte — xml.Header prepend (RESEARCH Pitfall 2)"
  - "render.Hash(files []ConfigFile) string — length-prefixed sha256 config hash (RENDER-09/D-08)"
affects: [01-03-shibboleth2-renderer, 01-04-attribute-map-renderer, phase-2-spinstance-controller]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Post-process regex collapse of encoding/xml's paired empty-element output into self-closing form, anchored on a closing-tag-name backreference so chardata-bearing elements never match"
    - "Length-prefixed (4-byte big-endian) name+bytes concatenation before hashing, to remove naive-concatenation ambiguity between different (filename,content) splits"

key-files:
  created:
    - internal/render/xmlformat.go
    - internal/render/xmlformat_test.go
    - internal/render/confighash.go
    - internal/render/confighash_test.go
  modified: []

key-decisions:
  - "confighash.go does NOT redeclare ConfigFile — it already exists in types.go from plan 01-01 (added there in anticipation of this plan); confighash.go adds only Hash to avoid a duplicate-type compile error"

requirements-completed: [RENDER-09]

coverage:
  - id: D1
    description: "collapseEmptyElements rewrites encoding/xml's paired empty-element output (<Foo attrs...></Foo>) into self-closing form (<Foo attrs.../>), leaving chardata-bearing and mismatched-name elements untouched"
    requirement: "RENDER-09"
    verification:
      - kind: unit
        ref: "internal/render/xmlformat_test.go#TestCollapseEmptyElements"
        status: pass
      - kind: unit
        ref: "internal/render/xmlformat_test.go#TestMarshalIndentDoesNotSelfClose"
        status: pass
    human_judgment: false
  - id: D2
    description: "withXMLProlog prepends the XML declaration (xml.Header) ahead of a marshaled body, since encoding/xml never emits it itself"
    verification:
      - kind: unit
        ref: "internal/render/xmlformat_test.go#TestXMLProlog"
        status: pass
    human_judgment: false
  - id: D3
    description: "render.Hash computes a deterministic, length-prefixed sha256 digest over ConfigFile entries, provably covering attribute-map.xml bytes (D-08)"
    requirement: "RENDER-09"
    verification:
      - kind: unit
        ref: "internal/render/confighash_test.go#TestConfigHash"
        status: pass
    human_judgment: false

duration: 3min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 2: XML Formatting Primitives and Config Hash Summary

**Byte-level XML self-closing-tag collapse + prolog prepend (`xmlformat.go`) and a length-prefixed sha256 config hash covering all three rendered artifacts including `attribute-map.xml` (`confighash.go`, RENDER-09/D-08).**

## Performance

- **Duration:** 3 min
- **Started:** 2026-07-11T14:52:49Z
- **Completed:** 2026-07-11T14:55:06Z
- **Tasks:** 2
- **Files modified:** 4 (all new)

## Accomplishments
- Empirically confirmed on the real Go 1.26 toolchain that `encoding/xml`'s `MarshalIndent` never self-closes an attributes-only, no-chardata element — it always emits the paired `<Foo x="1"></Foo>` form, never `<Foo x="1"/>` — via `TestMarshalIndentDoesNotSelfClose`
- Implemented `collapseEmptyElements`, a closing-tag-name-anchored regex post-process that rewrites genuinely-empty elements to self-closing form while leaving chardata-bearing elements (`<SSO ...>SAML2</SSO>`) and defensively-guarded mismatched-name matches untouched
- Implemented `withXMLProlog` to prepend `xml.Header` (which already includes the trailing newline matching the fixture format) ahead of a marshaled body
- Implemented `render.Hash`, a deterministic length-prefixed sha256 digest over `ConfigFile{Name, Bytes}` entries; proved via test that changing only `attribute-map.xml`'s bytes changes the hash (D-08 — that file is `reloadChanges="false"`, so an attribute-only change must still force a pod roll) and that the length-prefix scheme disambiguates the `("ab","c")` vs `("a","bc")` naive-concatenation collision case

## Task Commits

Each task followed the plan's RED/GREEN TDD gate:

1. **Task 1: Self-closing collapse + XML prolog (xmlformat.go) with empirical stdlib check**
   - `6a50ca6` test(01-02): add failing tests for xmlformat collapse/prolog helpers (RED)
   - `8279cc8` feat(01-02): add xmlformat collapse/prolog helpers (GREEN)
2. **Task 2: Length-prefixed config hash (confighash.go) — RENDER-09/D-08**
   - `66d3988` test(01-02): add failing test for length-prefixed config hash (RED)
   - `96ac831` feat(01-02): implement Hash for RENDER-09 config hash — D-08 (GREEN)

_Every task's RED commit was verified to fail (`go vet` reporting `undefined: <symbol>`) before its GREEN commit was created._

## Files Created/Modified
- `internal/render/xmlformat.go` - `collapseEmptyElements([]byte) []byte` + `withXMLProlog([]byte) []byte`; the one non-stdlib-obvious plumbing piece both `shibboleth2.go` and `attributemap.go` will depend on
- `internal/render/xmlformat_test.go` - `TestMarshalIndentDoesNotSelfClose` (empirical stdlib gap check), `TestCollapseEmptyElements` (positive/negative/defensive cases), `TestXMLProlog`
- `internal/render/confighash.go` - `Hash(files []ConfigFile) string`, length-prefixed sha256 digest; caller-fixed order (no internal sort)
- `internal/render/confighash_test.go` - `TestConfigHash`: determinism, attribute-map.xml change-sensitivity (D-08), length-prefix disambiguation

## Decisions Made
- `confighash.go` does not declare `ConfigFile` — plan 01-01 already added it to `types.go` in anticipation of this plan's needs (documented in 01-01-SUMMARY.md's decisions). Declaring it again in `confighash.go` per the plan's literal action text would have been a duplicate-type compile error; `confighash.go` adds only `Hash`, which is the actual net-new symbol this plan owns.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Did not redeclare `ConfigFile` in confighash.go**
- **Found during:** Task 2 (writing confighash.go)
- **Issue:** The plan's action text says "Create confighash.go defining `ConfigFile{Name string, Bytes []byte}` and `Hash(...)`" — but `ConfigFile` was already declared in `internal/render/types.go` by plan 01-01 (confirmed via `grep -n ConfigFile internal/render/types.go`, matching plan 01-01's frontmatter `files_modified` and its SUMMARY's key-decisions note that it was added early "so downstream renderer plans have a ready-made input shape"). Writing a second `type ConfigFile struct{...}` in confighash.go would be a duplicate-declaration compile error in the same package.
- **Fix:** confighash.go declares only `Hash(files []ConfigFile) string`, reusing the existing type from types.go.
- **Files modified:** internal/render/confighash.go (as planned; no change to types.go)
- **Verification:** `go build ./...` and `go vet ./...` both exit clean; `TestConfigHash` passes.
- **Committed in:** 96ac831 (Task 2 GREEN commit)

---

**Total deviations:** 1 auto-fixed (1 bug-avoidance)
**Impact on plan:** No scope creep — the plan's `must_haves.artifacts` (xmlformat.go, confighash.go) and both tasks' acceptance criteria are satisfied exactly as written; only the literal "defining ConfigFile" phrase in Task 2's action text was adjusted to avoid a duplicate declaration the plan's own dependency (01-01) had already resolved.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `collapseEmptyElements` and `withXMLProlog` are ready for plans 03/04 (`shibboleth2.go`, `attributemap.go`) to call directly from their `Render` functions — this was the one genuinely new, non-stdlib-obvious problem in the phase, now isolated and unit-tested in its own file.
- `render.Hash` is ready for plan 05/07 and Phase 2's SPInstance controller to call with the three rendered `ConfigFile`s in the fixed order (shibboleth2.xml, nginx.conf, attribute-map.xml); the D-08 attribute-map.xml-inclusion requirement is now enforced by a passing test, not just a comment.
- The k8s-free dependency boundary (D-01/D-02) still holds: `go list -deps ./internal/render/ | grep -E 'k8s.io|api/v1alpha1'` returns zero matches after this plan's additions.
- No blockers.

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*

## Self-Check: PASSED

All 4 created files confirmed present on disk; all 4 task commits (6a50ca6, 8279cc8, 66d3988, 96ac831) confirmed present in `git log --oneline --all`.
