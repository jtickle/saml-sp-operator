---
phase: 01-shared-render-aggregation-package
plan: 05
subsystem: render
tags: [go, text-template, nginx, fastcgi, header-hygiene]

# Dependency graph
requires:
  - phase: 01-shared-render-aggregation-package (plan 01)
    provides: "SPConfig/AttachmentModel/ClearListSpec types, DeriveSelfURL (RENDER-02)"
  - phase: 01-shared-render-aggregation-package (plan 04)
    provides: "AttributeMapping.Header -> attribute-map.xml id= convention (attributemap.go: `ID: a.Header`) that ClearList's Traefik-enumerate header naming must stay consistent with"
provides:
  - "render.RenderNginxConf(cfg SPConfig) ([]byte, error) — nginx.conf FastCGI->HTTP adapter renderer via text/template (RENDER-07)"
  - "render.validateHostname(h string) error — allowlist guard applied before every text/template Execute in this package"
  - "render.ClearList(model AttachmentModel, attrs []AttributeMapping) (ClearListSpec, error) — per-attachment-model edge header-hygiene value (RENDER-08)"
  - "internal/render/testdata/golden/nginx.conf — the byte-compare golden fixture for this and any future nginx.conf-touching plan"
affects: [01-06-confighash-wiring, 01-07-shibd-load-test, phase-2-spinstance-controller, phase-5-traefik-middleware]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "text/template guard pattern: every CRD-derived string that could reach a template is allowlist-validated (validHostnameRE, ^[a-zA-Z0-9.-]+$) BEFORE tmpl.Execute, never escaped after the fact — text/template has no auto-escaping of its own (RESEARCH.md Code Examples). Applied even though the sample template never literally interpolates the validated hostname (every host reference in nginx.conf's output is nginx's own $host runtime variable, matching the repo-root fixture), as a forward guard against a future template revision that DOES interpolate a literal host."
    - "ClearList as a pure value computation with no golden-file compare — RENDER-08's nginx-glob branch has zero consumer in this repo (RESEARCH.md Pitfall 6); nginxconf.go's template carries no clear-list directive at all."

key-files:
  created:
    - internal/render/nginxconf.go
    - internal/render/nginxconf_test.go
    - internal/render/clearlist.go
    - internal/render/clearlist_test.go
    - internal/render/testdata/golden/nginx.conf
  modified: []

key-decisions:
  - "AttributeMapping.Header field-naming ambiguity flagged for phase verification: types.go's doc comment describes AttributeMapping.Header as \"the request header exported to the app,\" but 01-04's attributemap.go actually uses it as the bare SAML attribute id (`ID: a.Header` -> attribute-map.xml's <Attribute id=...>), and the FastCGI authorizer is what prepends \"Variable-\" at export time (per attribute-map.xml's own repo-root comment: 'the FastCGI shibauthorizer exports each decoded attribute ... as an HTTP RESPONSE header named \"Variable-<id>\"'). This plan implements ClearList consistently with 01-04's usage (Header = bare id, ClearList prepends \"Variable-\" itself) rather than silently reinterpreting the field, per this plan's explicit cross-plan-consistency instruction. A future plan touching types.go should consider tightening AttributeMapping.Header's doc comment to state it holds the bare id, not a pre-formatted header name."
  - "nginx.conf's golden fixture uses SampleSPConfig() (fixtures_test.go, ExternalURL https://sp.example.com:30443) directly rather than a plan-local sample struct — unlike plans 03/04's locality convention (goldenShibboleth2SPConfig / goldenAttributeMapAttrs), because RenderNginxConf's only input surface from SPConfig is ExternalURL (via DeriveSelfURL), so there is no attribute-set/hostname-shape divergence risk that would justify a separate local fixture."
  - "validateHostname is called on DeriveSelfURL(cfg.ExternalURL)'s derived Name even though the current nginx.conf template never interpolates that value literally (every host reference in the rendered output is nginx's own $host runtime variable, matching the repo-root fixture's own documented rationale for using $host instead of a literal). This is intentional forward defense per T-05-01: a future template revision that adds a literal host interpolation must not silently lose the allowlist guard."
  - "ClearList's Traefik-enumerate header order is Variable-REMOTE_USER first, followed by one Variable-<Header> per attribute in input slice order (never a Go map range) — REMOTE_USER first because it is the principal identity header present regardless of which attributes an app declares, a stable ordering choice not otherwise specified by the plan."

patterns-established:
  - "The text/template allowlist-before-Execute guard (validateHostname) is the reference pattern for any future template-based renderer in this package that takes a CRD-derived free-text field."

requirements-completed: [RENDER-07, RENDER-08]

coverage:
  - id: D1
    description: "RenderNginxConf renders nginx.conf via text/template byte-for-byte against the project-native golden fixture, with SERVER_PORT/HTTP_HOST carrying the same external port shibboleth2.xml's handlerURL uses (spike fixes M/N)"
    requirement: "RENDER-07"
    verification:
      - kind: unit
        ref: "internal/render/nginxconf_test.go#TestRenderNginxConf"
        status: pass
    human_judgment: false
  - id: D2
    description: "Any CRD-derived string interpolated into an nginx directive is validated by validateHostname's allowlist regex BEFORE tmpl.Execute; a hostname failing the allowlist returns a non-nil error"
    requirement: "RENDER-07"
    verification:
      - kind: unit
        ref: "internal/render/nginxconf_test.go#TestRenderNginxConfHostileHostname"
        status: pass
    human_judgment: false
  - id: D3
    description: "ClearList(TraefikForwardAuth, attrs) returns an explicit enumerated Variable-REMOTE_USER + Variable-<Header> header list with an empty Glob; ClearList(NginxAuthRequest, attrs) returns a Variable-* Glob with no enumerated Headers; an unrecognized AttachmentModel returns a non-nil error"
    requirement: "RENDER-08"
    verification:
      - kind: unit
        ref: "internal/render/clearlist_test.go#TestClearList"
        status: pass
    human_judgment: false

duration: 20min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 5: nginx.conf Renderer + Edge Clear-List Summary

**`RenderNginxConf` (text/template) producing the FastCGI->HTTP adapter's nginx.conf byte-for-byte against a project-native golden fixture with a shared external-port value, plus `ClearList` computing the per-attachment-model header-hygiene value consumed by both the Traefik-ForwardAuth and future nginx-auth_request attachment models (RENDER-07/RENDER-08).**

## Performance

- **Duration:** 20 min
- **Started:** 2026-07-11T16:20:00Z
- **Completed:** 2026-07-11T16:38:17Z
- **Tasks:** 2
- **Files modified:** 5 (all new)

## Accomplishments
- Implemented `RenderNginxConf` over a `text/template` (D-04) reproducing the repo-root `nginx.conf`'s FastCGI->HTTP adapter structure: the `/healthz` 200, the `/Shibboleth.sso` block with `fastcgi_param HTTPS/SERVER_PORT/SERVER_NAME/HTTP_HOST` overrides preceding `include fastcgi_params`, the `/shibboleth-sp` alias, and the `/authcheck` block rewriting `X-Forwarded-*` into FastCGI params before its own `include fastcgi_params`
- The rendered external port is sourced from `DeriveSelfURL(cfg.ExternalURL).Port` — the same computed value `shibboleth2.xml`'s `handlerURL` uses — proven by `TestRenderNginxConf` asserting the port appears in `SERVER_PORT <port>;`, not a second independently-typed literal (spike fixes M/N, D-11's fail-open guard)
- Applied `validateHostname` (allowlist `^[a-zA-Z0-9.-]+$`) to the derived hostname BEFORE `tmpl.Execute`, proven by `TestRenderNginxConfHostileHostname`'s negative case — `text/template` performs no auto-escaping of its own (T-05-01)
- Implemented `ClearList(model, attrs)` as a pure-Go value computation (RESEARCH.md Pitfall 6, no phantom `nginx.conf` coupling): `TraefikForwardAuth` enumerates `Variable-REMOTE_USER` plus `"Variable-" + AttributeMapping.Header` for each attribute; `NginxAuthRequest` returns the `Variable-*` glob; an unrecognized model returns a non-nil error rather than defaulting
- Kept `ClearList`'s header naming consistent with 01-04's `attributemap.go` (`ID: a.Header`) per this plan's explicit cross-plan-consistency requirement, flagging the underlying `AttributeMapping.Header` doc-comment ambiguity for phase verification rather than silently reinterpreting the field (see `key-decisions`)

## Task Commits

Each task followed the plan's RED/GREEN TDD gate:

1. **Task 1: Render nginx.conf via text/template (RenderNginxConf) — RENDER-07**
   - `f883356` test(01-05): add failing test + golden nginx.conf fixture for RenderNginxConf (RED)
   - `97428bd` feat(01-05): implement RenderNginxConf via text/template (GREEN) — RENDER-07
2. **Task 2: Edge header-hygiene clear-list value (clearlist.go) — RENDER-08**
   - `66dca0d` test(01-05): add failing test for ClearList (RED)
   - `9adcb0f` feat(01-05): implement ClearList edge header-hygiene value (GREEN) — RENDER-08

_Both tasks' RED commits were verified to fail (`go test` reporting `undefined: RenderNginxConf` / `undefined: ClearList`) before their GREEN commits were created — the golden `nginx.conf` fixture was hand-authored to match the template source verbatim (deterministic string substitution has no marshaling-order ambiguity, unlike `encoding/xml`, so no empirical-generation step was needed the way plans 03/04 required for their XML fixtures)._

## Files Created/Modified
- `internal/render/nginxconf.go` - `RenderNginxConf`/`validateHostname` + the `nginxConfTemplate`/`nginxConfData`/`nginxConfTemplateSrc` template plumbing
- `internal/render/nginxconf_test.go` - `TestRenderNginxConf` (golden byte-compare + external-port assertion), `TestRenderNginxConfHostileHostname` (allowlist rejection negative case)
- `internal/render/clearlist.go` - `ClearList` + `remoteUserHeader` constant
- `internal/render/clearlist_test.go` - `TestClearList` (Traefik-enumerate, nginx-glob, unknown-model subtests)
- `internal/render/testdata/golden/nginx.conf` - the byte-compare target: single external port (`30443`, matching `SampleSPConfig`), spike prose comments trimmed per the plan's byte-target scope note

## Decisions Made
- `nginx.conf`'s golden fixture reuses `SampleSPConfig()` directly rather than a plan-local sample struct — see `key-decisions` for rationale (no attribute-set/hostname-shape divergence risk for this renderer)
- `validateHostname` is applied even though the current template has no literal host interpolation — forward defense per T-05-01, documented in `nginxconf.go`'s package doc comment
- `ClearList`'s Traefik-enumerate order is `Variable-REMOTE_USER` first, then attributes in input slice order — see `key-decisions`
- The `AttributeMapping.Header` field-naming ambiguity (types.go doc comment vs. actual bare-id usage established by 01-04) is documented here per this plan's explicit instruction, not silently resolved — flagged for phase verification

## Deviations from Plan

None - plan executed exactly as written. Both tasks' `<behavior>`, `<action>`, and `<acceptance_criteria>` were implemented as specified; the one underspecified detail (exact Traefik-enumerate header ordering, since the plan only requires the *set* of headers be correct) was resolved as documented above and is not a deviation from any explicit plan instruction.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `RenderNginxConf`'s output bytes are ready to be included in `confighash.go`'s `Hash` input set by a future caller (D-08) — this plan does not wire that call site itself (out of this plan's `files_modified` scope, same convention 01-04 followed for `attribute-map.xml`)
- `ClearList`'s `TraefikForwardAuth` branch is ready for Phase 5's Middleware `authResponseHeaders`/clear-header emission; the `NginxAuthRequest` branch is ready for the future standalone single-container tool (D-02) — neither consumer exists yet, as scoped
- The k8s-free dependency boundary (D-01/D-02) still holds: `go list -deps ./internal/render/ | grep -E 'k8s.io|api/v1alpha1'` returns zero matches after this plan's additions
- The `AttributeMapping.Header` doc-comment vs. actual-usage ambiguity (see `key-decisions`) should be resolved explicitly (either tighten the doc comment, or rename the field) before Phase 2/5 controller code starts consuming it, to avoid a future contributor reintroducing a "Variable-" double-prefix bug
- No blockers.

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*

## Self-Check: PASSED

All 5 created files confirmed present on disk (`internal/render/nginxconf.go`, `internal/render/nginxconf_test.go`, `internal/render/clearlist.go`, `internal/render/clearlist_test.go`, `internal/render/testdata/golden/nginx.conf`); all 5 commits (`f883356`, `97428bd`, `66dca0d`, `9adcb0f`, `cb9983f`) confirmed present in `git log --oneline --all`. `go build ./...`, `go vet ./...`, and `go test ./...` all exit clean.
