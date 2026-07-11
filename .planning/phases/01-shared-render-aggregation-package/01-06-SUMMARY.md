---
phase: 01-shared-render-aggregation-package
plan: 06
subsystem: render
tags: [go, encoding-xml, security-testing, property-testing, injection-safety]

# Dependency graph
requires:
  - phase: 01-shared-render-aggregation-package (plan 01)
    provides: "SPConfig/AppBinding/AttributeMapping/ConfigFile types, Resolve"
  - phase: 01-shared-render-aggregation-package (plan 02)
    provides: "Hash (D-08 config-hash) — this plan's determinism proof calls it directly over real renderer output"
  - phase: 01-shared-render-aggregation-package (plan 03)
    provides: "RenderShibboleth2 — the primary target of this plan's adversarial injection sweep"
  - phase: 01-shared-render-aggregation-package (plan 04)
    provides: "RenderAttributeMap — the second adversarial-sweep target"
  - phase: 01-shared-render-aggregation-package (plan 05)
    provides: "RenderNginxConf + validateHostname — the hostile-hostname rejection this plan asserts end to end"
provides:
  - "render.sanitizeComment(v string) string — D-05's input-layer \"--\" strip, ready for a future XML-comment call site"
  - "inject_test.go#TestInjectionSafety — cross-cutting proof that hostile CRD strings never abort or corrupt the XML render pipeline (RENDER-10, ROADMAP crit 3)"
  - "determinism_test.go#TestConfigHashStability — cross-cutting proof that the full render+hash pipeline is byte-identical on repeat and reorder-stable across 50+ shuffles (RENDER-09, ROADMAP crit 4)"
affects: [phase-2-spinstance-controller, phase-5-traefik-middleware]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Mutation-style adversarial testing: each hostile token is injected into exactly one string field of an otherwise-valid sample input at a time, isolating which field (if any) could break the render — never a single combined fuzz blob."
    - "In-test escape verification via decode round-trip, not source-file grep: a hostile token is proven neutralized by re-decoding the rendered bytes via encoding/xml.Decoder and finding the token intact inside a CharData/attribute Value — proving it survived as data, never as structural markup."
    - "Fixed-seed shuffle for a determinism proof (rand.New(rand.NewSource(1))): the adversarial test asserting order-independence must itself stay deterministic across CI runs, or a flake there would defeat the very property it's proving."

key-files:
  created:
    - internal/render/sanitize.go
    - internal/render/sanitize_test.go
    - internal/render/inject_test.go
    - internal/render/determinism_test.go
  modified: []

key-decisions:
  - "sanitizeComment has no call site in this package as of this plan: shibboleth2.go (plan 03) documents in its own package comment that the golden fixture is deliberately comment-free and no element in its tree is rendered via a `,comment` struct tag. The plan's action text was explicitly conditional (\"If shibboleth2.go routes any CRD-derived string into a comment...\") — since it doesn't, sanitize.go ships as a ready-to-use, fully-tested guard for the first future contributor who adds an operator-generated XML comment, per D-05/RESEARCH.md Pitfall 4's own guidance."
  - "TestInjectionSafety and TestConfigHashStability required zero production code changes beyond sanitizeComment — both tasks' `files_modified` scope in the plan was test-file-only, and both passed on first run against the existing plan 01-05 implementation. This is the expected/intended outcome for this plan: it is a cross-cutting adversarial + property proof over already-built renderers (encoding/xml's auto-escaping, validateHostname's allowlist, and resolve.go/all three renderers' never-range-a-map discipline), not new feature work. No RED phase was possible for these two tasks since there was no missing behavior to make pass — see Deviations."
  - "TestInjectionSafety's control-character token (roundTrips=false) is asserted only for well-formedness and no-error, not decoded-value identity: encoding/xml's EscapeText substitutes invalid XML 1.0 characters with U+FFFD rather than a numeric character reference (verified empirically), so the exact original bytes do not survive a round trip — that substitution IS the safety property being proven for this token, not a defect."
  - "The hostile-hostname-rejection subtest inside TestInjectionSafety intentionally duplicates 01-05's TestRenderNginxConfHostileHostname coverage (same underscore-hostname technique, plus two additional URL-legal-but-allowlist-illegal variants) — this plan's whole purpose is a cross-cutting proof net over every entrypoint in one place, so some overlap with per-renderer unit tests from earlier plans is expected, not redundant scope creep."

requirements-completed: [RENDER-09, RENDER-10]

coverage:
  - id: D1
    description: "sanitizeComment(v) collapses every \"--\" substring to \"-\" for any input, including odd-length dash runs, so a value routed into a struct-tag `,comment` XML field can never trip encoding/xml's \"comments must not contain --\" marshal error (D-05)"
    requirement: "RENDER-10"
    verification:
      - kind: unit
        ref: "internal/render/sanitize_test.go#TestSanitizeComment"
        status: pass
    human_judgment: false
  - id: D2
    description: "Hostile tokens (<, >, &, \", ', --, ]]>, a combo, and a NUL-free control-character string) injected one at a time into every free-text field of SPConfig/AppBinding/AttributeMapping never cause RenderShibboleth2 or RenderAttributeMap to return an error, and the rendered output always re-parses as well-formed XML with the hostile value surviving intact as decoded attribute/chardata text (never as raw structural markup)"
    requirement: "RENDER-10"
    verification:
      - kind: unit
        ref: "internal/render/inject_test.go#TestInjectionSafety"
        status: pass
    human_judgment: false
  - id: D3
    description: "A hostile external hostname (URL-legal per net/url, but failing validateHostname's [a-zA-Z0-9.-] allowlist) makes RenderNginxConf return an error rather than interpolating it unescaped into an nginx directive"
    requirement: "RENDER-10"
    verification:
      - kind: unit
        ref: "internal/render/inject_test.go#TestInjectionSafety/RenderNginxConf/hostile-hostname-rejected"
        status: pass
    human_judgment: false
  - id: D4
    description: "Rendering the same SPConfig + []AppBinding + []AttributeMapping twice produces byte-identical shibboleth2.xml, nginx.conf, and attribute-map.xml, and an identical combined config hash"
    requirement: "RENDER-09"
    verification:
      - kind: unit
        ref: "internal/render/determinism_test.go#TestConfigHashStability/render-twice-byte-identical"
        status: pass
    human_judgment: false
  - id: D5
    description: "Shuffling the []AppBinding input order across 50 distinct permutations never changes the combined config hash — Resolve + all three renderers are order-independent end to end (no map-ranging leak, ROADMAP crit 4)"
    requirement: "RENDER-09"
    verification:
      - kind: unit
        ref: "internal/render/determinism_test.go#TestConfigHashStability/reorder-stable-across-shuffles"
        status: pass
    human_judgment: false
  - id: D6
    description: "Changing a semantically-meaningful field (an AttributeMapping.Name / attribute id, or SPConfig.EntityID) always changes the config hash — reorder-stability isn't masking a hash that never changes at all"
    requirement: "RENDER-09"
    verification:
      - kind: unit
        ref: "internal/render/determinism_test.go#TestConfigHashStability/semantic-change-changes-hash"
        status: pass
    human_judgment: false

duration: 7min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 6: Adversarial + Determinism Proof Net Summary

**`TestInjectionSafety` (mutation-injects `<`, `>`, `&`, `"`, `'`, `--`, `]]>`, and a control-char string into every free-text field of the real `RenderShibboleth2`/`RenderAttributeMap`/`RenderNginxConf` entrypoints and proves well-formed re-parse) and `TestConfigHashStability` (proves the full Resolve→render→Hash pipeline is byte-identical on repeat and reorder-stable across 50 `[]AppBinding` shuffles), plus `sanitizeComment` — the D-05 `--`-strip guard ready for a future XML-comment call site — closing out RENDER-09 and RENDER-10 end to end.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-07-11T16:41:29Z
- **Completed:** 2026-07-11T16:48:00Z
- **Tasks:** 3
- **Files modified:** 4 (all new)

## Accomplishments
- Implemented `sanitizeComment` (D-05): collapses every `"--"` substring to `"-"` via a `strings.Contains`/`ReplaceAll` loop (a single pass leaves `"---"` as `"--"` — the loop is required, not cosmetic), proven for even/odd-length dash runs, scattered pairs, already-clean input, and empty input
- Proved RENDER-10 end to end with a mutation-style adversarial sweep (`TestInjectionSafety`) over the REAL renderers from plans 03-05: 9 hostile tokens × 12 free-text fields (`SPConfig.EntityID`, `IdP.MetadataURL`/`EntityID`, `CredentialKeyPath`/`CredentialCertPath`, `RemoteUser`, `Sessions.RelayState`/`CookieProps`, `AppBinding.Hostname`/`Path`, `AttributeMapping.Name`/`Header`) — 108 cases, every one asserting no marshal error and a well-formed re-parse, with the hostile value proven to survive as decoded attribute/chardata text (never raw structural markup) for every printable-XML-char token
- Proved the NUL-free control-character token is neutralized differently but still safely: encoding/xml's `EscapeText` substitutes U+FFFD for invalid XML 1.0 characters rather than emitting them raw or erroring — verified empirically before writing the assertion, so the test encodes actual stdlib behavior rather than an assumption
- Proved `RenderNginxConf` rejects three URL-legal-but-allowlist-illegal hostile hostnames via `validateHostname` (underscore, shell-metacharacter, and quote/semicolon variants)
- Proved RENDER-09 end to end with `TestConfigHashStability`: the full `Resolve → RenderShibboleth2/RenderNginxConf/RenderAttributeMap → Hash` pipeline is byte-identical across two renders of the same input, the combined hash is identical across 50 fixed-seed shuffles of `[]AppBinding` input order, and the hash changes when an attribute id or `EntityID` changes (proving reorder-stability isn't masking a hash that never changes)

## Task Commits

Each task followed the plan's RED/GREEN TDD gate where applicable:

1. **Task 1: Input-layer `--` sanitizer (sanitize.go) — D-05**
   - `16e3447` test(01-06): add failing test for sanitizeComment (RED) — D-05
   - `6f6b72e` feat(01-06): implement sanitizeComment input-layer -- strip (GREEN) — D-05
2. **Task 2: Adversarial injection safety across all renderers (inject_test.go) — RENDER-10**
   - `fc39590` test(01-06): add adversarial injection-safety proof over real renderers — RENDER-10
3. **Task 3: Pipeline determinism + hash reorder-stability (determinism_test.go) — RENDER-09**
   - `ef5038e` test(01-06): add config-hash reorder-stability + determinism proof — RENDER-09

**Plan metadata:** captured in this SUMMARY commit.

_Task 1's RED commit was verified to fail (`go vet`/`go test` reporting `undefined: sanitizeComment`) before the GREEN commit was created. Tasks 2 and 3 are test-only per their `files_modified` scope in the plan (no production file to implement) — see Deviations for why no RED phase applied to either._

## Files Created/Modified
- `internal/render/sanitize.go` - `sanitizeComment(v string) string` (D-05's `--`-strip loop) + package doc explaining the two-code-path comment-guard pitfall and why this file has no call site yet
- `internal/render/sanitize_test.go` - `TestSanitizeComment` (even/odd-length dash runs, scattered pairs, already-clean, empty), `TestSanitizeCommentUnchangedWhenAlreadyClean`
- `internal/render/inject_test.go` - `TestInjectionSafety` (hostile-token × field mutation matrix over `RenderShibboleth2`/`RenderAttributeMap`, plus a `RenderNginxConf` hostile-hostname subtest), `assertWellFormedXML`, `assertTokenPresentAsText` helpers
- `internal/render/determinism_test.go` - `TestConfigHashStability` (render-twice, 50-shuffle reorder-stability, semantic-change-changes-hash subtests), `renderAll` helper

## Decisions Made
- `sanitizeComment` ships with no call site — see `key-decisions` above; it is ready-to-use defense-in-depth for the first future contributor who routes a CRD-derived string into a `,comment`-tagged XML field
- Tasks 2 and 3 required zero production code changes: both passed against the existing plan 01-05 implementation on first run, confirming `encoding/xml`'s auto-escaping, `validateHostname`'s allowlist, and the never-range-a-map discipline already established in plans 01-05 already satisfy RENDER-09/RENDER-10 end to end — see `key-decisions` and Deviations
- The control-character hostile token is asserted only for well-formedness/no-error, not decoded-value identity, because `encoding/xml.EscapeText` substitutes U+FFFD for invalid XML 1.0 characters rather than round-tripping the exact original bytes — verified empirically before writing the assertion
- Escape-safety is proven via in-test decode round-trip (re-parsing rendered bytes with `encoding/xml.Decoder` and finding the hostile token intact as CharData/attribute text), never via a source-file grep, per the plan's explicit instruction

## Deviations from Plan

**1. [Not a deviation — plan-anticipated outcome] Tasks 2 and 3 had no RED phase**

Both Task 2 (`inject_test.go`) and Task 3 (`determinism_test.go`) are scoped in the plan's own `files_modified` as test-file-only additions — the plan names no production file for either task to implement, because this plan's stated purpose is proving properties (RENDER-09 reorder-stability, RENDER-10 injection-safety) that the plan 01-05 implementations already satisfy by construction (`encoding/xml` auto-escaping + `validateHostname` allowlist + never-range-a-map discipline). Both test suites passed on first run with zero production code changes. This is the plan working as designed, not a shortfall: the standard TDD RED requirement ("write a failing test first") does not apply when there is no missing behavior to make pass — see the general TDD execution guidance's error-handling note ("RED doesn't fail → investigate") for exactly this case; investigation confirmed the existing implementation is correct, not that the test was wrong.

No auto-fixed issues (Rules 1-3) were required in this plan — no bug, missing critical functionality, or blocking issue was discovered in the plans 01-05 implementation while writing this plan's adversarial/property tests.

---

**Total deviations:** 0 auto-fixed; 1 documented plan-anticipated TDD-gate note (see above)
**Impact on plan:** None — plan executed exactly as written for all three tasks.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- RENDER-09 and RENDER-10 are both closed end to end: the whole `internal/render` package (plans 01-06) is proven injection-safe and deterministic/reorder-stable across every renderer, ready for Phase 2's controller to consume `Resolve`/`RenderShibboleth2`/`RenderAttributeMap`/`RenderNginxConf`/`Hash` without re-deriving these guarantees
- `sanitizeComment` is ready and tested for the first future contributor who adds an operator-generated XML comment (e.g. a "rendered by saml-sp-operator, do not edit" marker) — route that value through `sanitizeComment` before a `,comment`-tagged struct field, never through manual `xml.EncodeToken(xml.Comment(...))`
- `TestInjectionSafety` and `TestConfigHashStability` are reusable regression nets: any future change to a renderer, `Resolve`, or `Hash` that reintroduces a map-range or weakens escaping will fail one of these tests immediately, before it reaches a controller
- The k8s-free dependency boundary (D-01/D-02) still holds: `go list -deps ./internal/render/ | grep -E 'k8s.io|api/v1alpha1'` returns zero matches after this plan's additions
- No blockers. This is the last plan of Phase 1's shared render/aggregation package (wave 3, depends on all of 01-01..05).

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*
