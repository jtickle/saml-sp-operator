---
phase: 01-shared-render-aggregation-package
plan: 03
subsystem: render
tags: [go, encoding-xml, shibboleth-sp, golden-fixture]

# Dependency graph
requires:
  - phase: 01-shared-render-aggregation-package (plan 01)
    provides: "SPConfig/AppBinding types, DeriveSelfURL (RENDER-02), Resolve (RENDER-06)"
  - phase: 01-shared-render-aggregation-package (plan 02)
    provides: "collapseEmptyElements/withXMLProlog (xmlformat.go) — self-closing-tag + prolog post-processing RenderShibboleth2 depends on"
provides:
  - "render.RenderShibboleth2(cfg SPConfig, winners []AppBinding) ([]byte, error) — full Shibboleth SP v3 shibboleth2.xml renderer (RENDER-01)"
  - "internal/render/testdata/golden/shibboleth2.xml — the byte-compare golden fixture for this and any future shibboleth2.xml-touching plan"
  - "buildRequestMapHosts/pathDepth — RequestMap aggregation+ordering helpers (RENDER-04) reusable if a future plan needs the same grouping logic"
affects: [01-04-attribute-map-renderer, 01-05-nginx-conf-renderer, 01-06-clearlist-confighash, 01-07-shibd-load-test, phase-2-spinstance-controller]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-03 literal-xmlns-attribute approach applied concretely: xmlns/xmlns:conf declared as plain string attrs on the spConfigXML root struct only; every descendant struct carries a bare local-name xml tag (empty Space) — verified byte-identical to the golden fixture, no xml.Name{Space} anywhere"
    - "RequestMap grouping: winners are grouped by Hostname (never map-ranged — slices.SortFunc on a cloned slice), single-binding whole-host groups (Path '' or '/') flatten authType/requireSession onto <Host> directly (matches the spike's single-app shape); multi-path groups nest <Path> children ordered most-specific(deepest)-path-first"
    - "Structural elements with no SPConfig field (Errors, MetadataProvider.backingFilePath/maxRefreshDelay, AttributeExtractor/Resolver/Filter, SecurityPolicyProvider, ProtocolProvider, clockSkew, Handler ACLs) are rendered as fixed Go constants in shibboleth2.go, not new SPConfig fields — this plan's files_modified is scoped to shibboleth2.go/_test.go/testdata only"

key-files:
  created:
    - internal/render/shibboleth2.go
    - internal/render/shibboleth2_test.go
    - internal/render/testdata/golden/shibboleth2.xml
  modified: []

key-decisions:
  - "cfg.EntityID is rendered verbatim onto <ApplicationDefaults entityID=...> rather than reconstructed from DeriveSelfURL — SPConfig.EntityID's field contract (types.go, plan 01) already states it is the complete SAML entityID literal ('mirrors SPInstanceSpec.EntityID field-for-field'), and DeriveSelfURL's SelfURL type carries no path component to build a full entityID URL from. DeriveSelfURL is used for handlerURL and the RequestMap Host/port matching, which is where the fail-open bug (D-11, T-03-01) actually lives."
  - "No XML comment is rendered anywhere in this file (the golden fixture is deliberately comment-free per RESEARCH.md Pitfall 5), so the D-05 struct-tag ',comment' guard has no call site in shibboleth2.go today — documented in the file's package doc comment so a future contributor who adds an operator-generated comment knows which path to use (struct-tag ',comment', never manual xml.EncodeToken)."
  - "RequestMap Host/Path grouping and 'most-specific-path-first' ordering: since AppBinding has no regex-host field, RENDER-04's 'exact <Host> before <HostRegex>' is vacuously satisfied (every group renders as an exact <Host>, TestRequestMapOrdering asserts no <HostRegex substring is ever emitted). A host with exactly one winner whose Path is empty/'/' renders authType+requireSession directly on <Host> (matches the golden fixture's whole-host-protected shape); a host with more than one distinct Path renders nested <Path name=... authType=... requireSession=.../> children, sorted by path depth descending (most segments first) then lexicographically for a strict deterministic order — never a map range."

requirements-completed: [RENDER-01, RENDER-04, RENDER-05]

coverage:
  - id: D1
    description: "RenderShibboleth2 renders the full shibboleth2.xml tree (RequestMapper/RequestMap, ApplicationDefaults with REMOTE_USER, Sessions with fully-qualified handlerURL, SSO/Logout/Handlers, MetadataProvider, AttributeExtractor/Resolver/Filter, CredentialResolver, SecurityPolicyProvider, ProtocolProvider) byte-for-byte against the project-native golden fixture"
    requirement: "RENDER-01"
    verification:
      - kind: unit
        ref: "internal/render/shibboleth2_test.go#TestRenderShibboleth2"
        status: pass
    human_judgment: false
  - id: D2
    description: "RequestMap is aggregated from Resolve's Winners in deterministic order: exact <Host> before <HostRegex> (vacuously true — AppBinding has no regex-host input, asserted no <HostRegex is ever emitted), most-specific path first within a host"
    requirement: "RENDER-04"
    verification:
      - kind: unit
        ref: "internal/render/shibboleth2_test.go#TestRequestMapOrdering"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every RequestMap <Host> carries explicit scheme AND port even when the port is the scheme default (443) — the fail-open bug T-03-01/D-11 exists to prevent"
    requirement: "RENDER-05"
    verification:
      - kind: unit
        ref: "internal/render/shibboleth2_test.go#TestHostSchemePort"
        status: pass
    human_judgment: false

duration: 20min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 3: shibboleth2.xml Renderer Summary

**`RenderShibboleth2` (encoding/xml struct marshaling) producing the full Shibboleth SP v3 config tree byte-for-byte against a project-native golden fixture, with the RequestMap aggregated from `Resolve`'s Winners and every `<Host>` carrying explicit scheme+port even on default ports (RENDER-01/04/05).**

## Performance

- **Duration:** 20 min
- **Started:** 2026-07-11T16:03:00Z
- **Completed:** 2026-07-11T16:23:26Z
- **Tasks:** 2
- **Files modified:** 3 (all new)

## Accomplishments
- Empirically generated (not hand-simulated) the exact `xml.MarshalIndent` + `collapseEmptyElements` + `withXMLProlog` output for a fixed sample `SPConfig`/`AppBinding` and locked it as `internal/render/testdata/golden/shibboleth2.xml` — a comment-free, machine-formatted semantic-tree reproduction of the repo-root `shibboleth2.xml` (RESEARCH.md Pitfall 5 / Open Question 1 resolved as documented)
- Implemented `buildShibboleth2Tree`/`RenderShibboleth2` reproducing every element the repo-root fixture carries: `RequestMapper`/`RequestMap`/`Host`, `ApplicationDefaults` (entityID + space-joined REMOTE_USER), `Sessions` (fully-qualified handlerURL via the shared `DeriveSelfURL` value), `SSO`/`Logout` (chardata) + three self-closing `Handler`s, `Errors`, `MetadataProvider`, `AttributeExtractor`/`AttributeResolver`/`AttributeFilter`, `CredentialResolver`, and root-sibling `SecurityPolicyProvider`/`ProtocolProvider`
- Applied D-03's literal-`xmlns`-attribute approach concretely and verified byte-identical output: `xmlns`/`xmlns:conf` declared as plain string attrs on the root struct only, no child re-declares a namespace, no `xml.Name{Space}` anywhere in the file
- Built RequestMap aggregation from `Resolve`'s Winners without ever ranging a Go map: hosts grouped and sorted via `slices.SortFunc` on a cloned slice; single-binding whole-host groups flatten onto `<Host>` (matching the golden fixture's shape), multi-path groups nest `<Path>` children ordered most-specific(deepest)-path-first
- Every `<Host>` (and nested `<Path>`) always carries `scheme`+`port`, including the scheme-default port 443 — proven by `TestHostSchemePort`'s negative case, not just documented as intent

## Task Commits

Each task followed the plan's RED/GREEN TDD gate:

1. **Task 1: Author the project-native golden fixture (testdata/golden/shibboleth2.xml)**
   - `f05a2cb` test(01-03): add golden shibboleth2.xml fixture for RenderShibboleth2
2. **Task 2: Render shibboleth2.xml via encoding/xml (RenderShibboleth2) — RENDER-01/04/05**
   - `8792c6f` test(01-03): add failing tests for RenderShibboleth2 (RED)
   - `51c6522` feat(01-03): implement RenderShibboleth2 for shibboleth2.xml rendering (GREEN)

_Task 2's RED commit was verified to fail (`go vet`/`go test` reporting `undefined: RenderShibboleth2`/`undefined: buildShibboleth2Tree`) before the GREEN commit was created. Task 1's golden fixture bytes were generated empirically by running the (locally staged, not-yet-committed) renderer against the fixed sample input and capturing its actual output — not hand-typed — so the fixture and the RED/GREEN implementation are guaranteed consistent rather than independently guessed._

## Files Created/Modified
- `internal/render/shibboleth2.go` - `RenderShibboleth2`/`buildShibboleth2Tree` + the full XML struct tree (`spConfigXML`, `requestMapperXML`/`hostXML`/`pathXML`, `applicationDefaultsXML`, `sessionsXML`, `errorsXML`, `metadataProviderXML`, `attributeExtractorXML`/`attributeResolverXML`/`attributeFilterXML`, `credentialResolverXML`, `securityPolicyProviderXML`, `protocolProviderXML`) + `buildRequestMapHosts`/`pathDepth`/`boolAttr` helpers
- `internal/render/shibboleth2_test.go` - `TestRenderShibboleth2` (golden byte-compare), `TestRequestMapOrdering` (exact-Host-before-HostRegex + most-specific-path-first), `TestHostSchemePort` (default-port-443 negative case)
- `internal/render/testdata/golden/shibboleth2.xml` - the byte-compare target: single-host whole-host-protected sample (`app.example.com:30443`), comment-free, `xml.MarshalIndent(..., "", "    ")`-formatted

## Decisions Made
- `cfg.EntityID` is rendered verbatim rather than reconstructed from `DeriveSelfURL` — see `key-decisions` above; `DeriveSelfURL` is consumed only for `handlerURL` and the RequestMap host/port consistency, which is the actual fail-open surface (T-03-01/D-11)
- No XML comment is rendered anywhere in this file (golden fixture is deliberately comment-free); the D-05 `,comment` struct-tag guard has no call site today, documented in the package doc comment for a future contributor
- Structural elements absent from `SPConfig` (Errors, MetadataProvider's `backingFilePath`/`maxRefreshDelay`, AttributeExtractor/Resolver/Filter, SecurityPolicyProvider, ProtocolProvider, `clockSkew`, Handler ACLs) are rendered as fixed Go constants in `shibboleth2.go` rather than new `SPConfig` fields, matching this plan's `files_modified` scope (shibboleth2.go/_test.go/testdata only — no `types.go` changes)
- RequestMap host/path grouping+ordering implemented as described in `key-decisions`; `TestRequestMapOrdering` asserts both the nested-`<Path>` ordering and that no `<HostRegex` substring is ever emitted (there is no regex-host input on `AppBinding` in this phase)

## Deviations from Plan

None - plan executed exactly as written. Both tasks' `<behavior>`, `<action>`, and `<acceptance_criteria>` were implemented as specified; the underspecified detail (exact RequestMap `<Host>`/`<Path>` nesting shape for multi-path hosts, since `AppBinding` has no `IsRegex` field and the golden reference only exercises the single-whole-host case) was resolved per the plan's own RENDER-04 wording ("most-specific path first") and documented above as a decision, not a deviation from any explicit plan instruction.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `RenderShibboleth2` is ready for plan 06/07's `shibdload_test.go` to mount its output into a real `shibd` container (ROADMAP success criterion #1 — the authoritative correctness gate, not the golden byte-compare alone)
- `internal/render/testdata/golden/` now has a real fixture and precedent; plan 04 (`attributemap.go`) can follow the same "generate the golden bytes by running the implementation, then lock them" workflow used here
- The k8s-free dependency boundary (D-01/D-02) still holds: `go list -deps ./internal/render/ | grep -E 'k8s.io|api/v1alpha1'` returns zero matches after this plan's additions
- No blockers.

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*

## Self-Check: PASSED

All 3 created files confirmed present on disk (`internal/render/shibboleth2.go`, `internal/render/shibboleth2_test.go`, `internal/render/testdata/golden/shibboleth2.xml`); all 4 commits (`f05a2cb`, `8792c6f`, `51c6522`, `6261e5e`) confirmed present in `git log --oneline --all`. `go build ./...` and `go test ./internal/render/...` both exit clean.
