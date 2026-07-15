---
phase: 01-shared-render-aggregation-package
plan: 01
subsystem: render
tags: [go, encoding-free-types, cmp, slices, net-url]

# Dependency graph
requires: []
provides:
  - "internal/render package skeleton: plain-Go input/output types with zero k8s dependency"
  - "render.DeriveSelfURL (RENDER-02) — the shared self-URL value shibboleth2.go and nginxconf.go will both consume"
  - "render.Resolve (RENDER-06) — the shared deterministic collision function both controllers will import in Phase 3/4/5"
  - "internal/render/fixtures_test.go shared sample builders (SampleSPConfig, SampleAppBindings) reused by every downstream _test.go in this package"
affects: [01-02-shibboleth2-renderer, 01-03-attribute-map-renderer, 01-04-nginx-conf-renderer, 01-05-clearlist-confighash, 01-06-shibd-load-test, phase-3-appintegration-controller, phase-4-spinstance-controller, phase-5-aggregation]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "cmp.Or + slices.SortFunc multi-key comparator (priority desc, createdAt asc, UID asc) for a strict-total-order sort — SortFunc not SortStableFunc is intentional"
    - "Never range a Go map to build ordered output; always emit from a ranked/sorted slice (feeds RENDER-09 hash stability)"
    - "k8s-free plain-Go type boundary: controllers synthesize AppBinding from ObjectMeta/HTTPRoute before crossing into internal/render — no k8s.io or api/v1alpha1 import anywhere under internal/render"

key-files:
  created:
    - internal/render/types.go
    - internal/render/fixtures_test.go
    - internal/render/selfurl.go
    - internal/render/selfurl_test.go
    - internal/render/resolve.go
    - internal/render/resolve_test.go
  modified: []

key-decisions:
  - "SessionDefaults (SPConfig.Sessions) fields (LifetimeSeconds, TimeoutSeconds, RelayState, CheckAddress, HandlerSSL, CookieProps) were not explicitly enumerated in the plan's action text — filled per CONTEXT.md 'Claude's Discretion' from the shibboleth2.xml <Sessions> element (lines 67-69) so downstream renderer plans have a ready-made input shape."
  - "DeriveSelfURL rejects non-https and unparseable/empty external URLs with an error rather than defaulting, per the plan's explicit behavior spec — a silently-defaulted scheme/port would produce a RequestMap entry that fails open."
  - "Resolve(bindings) (Resolution, error) always returns a nil error in this implementation; the error return is kept per D-06's exact signature for forward compatibility with future input validation, not exercised by any current caller path."

requirements-completed: [RENDER-02, RENDER-06]

coverage:
  - id: D1
    description: "Plain-Go render package types (SPConfig, IdPConfig, SessionDefaults, AttributeMapping, AppBinding, Resolution, Conflict, AttachmentModel, ClearListSpec, ConfigFile) with zero k8s.io/api-v1alpha1 dependency"
    requirement: "RENDER-02"
    verification:
      - kind: unit
        ref: "internal/render/fixtures_test.go — SampleSPConfig/SampleAppBindings compile against declared struct fields"
        status: pass
      - kind: other
        ref: "go list -deps ./internal/render/ | grep -E 'k8s.io|api/v1alpha1' (zero matches)"
        status: pass
    human_judgment: false
  - id: D2
    description: "DeriveSelfURL derives a consistent, always-fully-qualified SelfURL (Scheme, Name, Port, HandlerURL) from an app's external URL, including non-standard ports (spike fixes M/N)"
    requirement: "RENDER-02"
    verification:
      - kind: unit
        ref: "internal/render/selfurl_test.go#TestSelfURLConsistency"
        status: pass
    human_judgment: false
  - id: D3
    description: "Resolve(bindings) deterministically picks the same (hostname,path) collision winner regardless of input order, with the same-second-CreatedAtUnix UID tiebreak and Priority-first ordering (D-06/D-07, ROADMAP crit 2)"
    requirement: "RENDER-06"
    verification:
      - kind: unit
        ref: "internal/render/resolve_test.go#TestResolveDeterminism"
        status: pass
    human_judgment: false

duration: 40min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 1: Types, Self-URL Derivation, Collision Resolution Summary

**Pure-Go `internal/render` package skeleton with `DeriveSelfURL` (RENDER-02, spike fixes M/N) and `Resolve` (RENDER-06, D-06/D-07 priority-desc/createdAt-asc/UID-asc collision resolution) — the two shared seams every other Phase 1 plan and both future controllers build on, with zero Kubernetes dependency proven by `go list -deps`.**

## Performance

- **Duration:** 40 min
- **Started:** 2026-07-11T12:04:07Z
- **Completed:** 2026-07-11T12:44:07Z
- **Tasks:** 3
- **Files modified:** 6 (all new)

## Accomplishments
- Established the plain-Go type model (`SPConfig`, `IdPConfig`, `SessionDefaults`, `AttributeMapping`, `AppBinding`, `Resolution`, `Conflict`, `AttachmentModel` + consts, `ClearListSpec`, `ConfigFile`) with zero `k8s.io`/`api/v1alpha1` imports, verified by `go list -deps`
- Implemented `DeriveSelfURL` so a default-port external URL still yields an explicit `Port` and a fully-qualified `HandlerURL` — never relying on bare-hostname auto-expansion (spike fix N) or a relative handler path (spike fix M)
- Implemented `Resolve` as a pure, deterministic collision function: `rankOrder` uses `cmp.Or` + `slices.SortFunc` with the exact `(priority desc, createdAt asc, UID asc)` key, then partitions ranked bindings into `Winners`/`Conflicts` by `(Hostname, Path)` without ever ranging a Go map
- Built shared `fixtures_test.go` sample builders (`SampleSPConfig`, `SampleAppBindings`) — the latter deliberately encodes a 3-way collision (same-second-tie pair + a higher-priority-but-older winner) plus two non-colliding bindings, reused directly by `resolve_test.go` and available to every downstream `_test.go` in this package

## Task Commits

Each task followed the plan's RED/GREEN TDD gate:

1. **Task 1: Plain-Go input/output types (types.go) + shared test fixtures**
   - `9973050` test(01-01): add fixtures_test.go for render package types (RED)
   - `b4a624c` feat(01-01): add plain-Go render package types (GREEN)
2. **Task 2: Self-URL derivation (selfurl.go) — RENDER-02**
   - `66961ad` test(01-01): add TestSelfURLConsistency for DeriveSelfURL (RED)
   - `15d7543` feat(01-01): implement DeriveSelfURL for RENDER-02 self-URL derivation (GREEN)
3. **Task 3: Deterministic collision resolution (resolve.go) — RENDER-06**
   - `72421bd` test(01-01): add TestResolveDeterminism for Resolve collision logic (RED)
   - `782c720` feat(01-01): implement Resolve for RENDER-06 deterministic collision resolution (GREEN)

_Every task's RED commit was verified to fail (`go vet`/`go test` failing with `undefined: <symbol>`) before its GREEN commit was created._

## Files Created/Modified
- `internal/render/types.go` - Plain-Go input/output structs mirroring the CRD semantic vocabulary (D-01/D-02); package doc records the zero-k8s-dependency contract
- `internal/render/fixtures_test.go` - `SampleSPConfig`/`SampleAppBindings` shared test fixtures, including a deliberate 3-way collision group
- `internal/render/selfurl.go` - `SelfURL` type + `DeriveSelfURL(externalURL string) (SelfURL, error)`, always explicit scheme+port and fully-qualified `HandlerURL`
- `internal/render/selfurl_test.go` - `TestSelfURLConsistency`: non-default port, default port (still explicit), non-https/unparseable/empty rejection
- `internal/render/resolve.go` - `Resolve(bindings []AppBinding) (Resolution, error)` + unexported `rankOrder`/`hostPathKey` helpers
- `internal/render/resolve_test.go` - `TestResolveDeterminism`: 100x shuffle byte-identical output, same-second UID tiebreak, priority-beats-age, non-colliding-all-win, and the fixture's 3-way collision composition

## Decisions Made
- `SessionDefaults`'s exact fields were left to Claude's discretion by CONTEXT.md; filled from the `shibboleth2.xml` `<Sessions>` element's attribute set (`lifetime`, `timeout`, `relayState`, `checkAddress`, `handlerSSL`, `cookieProps`) so the renderer plan (01-02) has a ready-made, XML-attribute-shaped input rather than needing to extend `SPConfig` later
- `DeriveSelfURL` treats "no port in the URL" and "explicit standard port" identically (both produce `Port: 443` + a port-bearing `HandlerURL`) — matches the plan's explicit behavior spec and spike fix N's "never rely on bare-hostname auto-expansion" requirement
- Kept `Resolve`'s `error` return per D-06's exact signature even though no code path in this plan populates it non-nil; documented in-code and in frontmatter so a future reader doesn't assume it's dead weight to remove

## Deviations from Plan

None - plan executed exactly as written. All three tasks' `<behavior>`, `<action>`, and `<acceptance_criteria>` were implemented as specified; `SessionDefaults`'s field shape was the only underspecified detail, and CONTEXT.md explicitly delegates exact struct layout to Claude's discretion.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `internal/render/types.go`, `selfurl.go`, and `resolve.go` are ready for Plan 02 (shibboleth2.xml renderer) to import directly — `SelfURL`/`DeriveSelfURL` is the shared self-URL value both `shibboleth2.go` and `nginxconf.go` must consume without re-deriving
- `fixtures_test.go`'s `SampleSPConfig`/`SampleAppBindings` are ready for reuse by every subsequent `_test.go` file in this package (renderer golden-file tests, clear-list tests, config-hash tests)
- No blockers. The k8s-free dependency boundary (D-01/D-02) is enforced by the actual `go list -deps` graph, not a comment convention, so a future accidental `k8s.io`/`api/v1alpha1` import in this package will show up immediately in that check.

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*

## Self-Check: PASSED

All 6 created files confirmed present on disk; all 6 task commits (9973050, b4a624c, 66961ad, 15d7543, 72421bd, 782c720) confirmed present in `git log --oneline --all`.
