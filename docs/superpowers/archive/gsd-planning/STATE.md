---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
current_phase: 2
current_phase_name: SPInstance Controller — Static Path & Production Foundations
status: planning
stopped_at: Phase 1 context gathered (assumptions mode)
last_updated: "2026-07-11T17:40:48.340Z"
last_activity: 2026-07-11
last_activity_desc: Phase 01 complete, transitioned to Phase 2
progress:
  total_phases: 1
  completed_phases: 1
  total_plans: 7
  completed_plans: 7
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-10)

**Core value:** Reconcile two CRDs (`SPInstance`, `AppIntegration`) + Gateway API `HTTPRoute`s into a working forward-auth SAML SP — an app team adds one CRD and their app is authenticated, cross-gateway-portable, with no hand-written `shibboleth2.xml`.
**Current focus:** Phase 01 — shared-render-aggregation-package

## Current Position

Phase: 2 — SPInstance Controller — Static Path & Production Foundations
Plan: Not started
Status: Ready to plan
Last activity: 2026-07-11 — Phase 01 complete, transitioned to Phase 2

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 7
- Average duration: n/a
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 7 | - | - |

**Recent Trend:**

- Last 5 plans: n/a
- Trend: n/a

*Updated after each plan completion*
| Phase 01 P01 | 40min | 3 tasks | 6 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- Roadmap: 6-phase dependency-ordered build (render package → SPInstance static → AppIntegration resolution-only → cross-namespace aggregation → Middleware/Conflict/finalizer → hardening closeout), per architecture research's Phase A–E sequence plus a final closeout phase.
- Roadmap: Cross-cutting production-operator concerns (leader election, RBAC/cache scoping, metrics, NetworkPolicy, CEL validation) front-loaded into Phase 2 alongside the first controller rather than deferred; SSRF guard, full-lifecycle Events audit, and RBAC least-privilege review held to Phase 6 since they're only meaningfully verifiable at full-system scope.
- REQUIREMENTS.md's stated "27 total" v1 requirements count was stale/incorrect — actual count is 31 (RENDER-01..10, SPI-01..05, APP-01..05, OBS-01..05, SEC-01..04, OPS-01..02). Corrected during roadmap traceability update; all 31 mapped, 0 unmapped.
- [Phase ?]: SessionDefaults fields (LifetimeSeconds, TimeoutSeconds, RelayState, CheckAddress, HandlerSSL, CookieProps) filled from shibboleth2.xml Sessions element per CONTEXT.md Claude's Discretion
- [Phase ?]: DeriveSelfURL rejects non-https/unparseable/empty external URLs with an error rather than defaulting (fail-closed, not fail-open)

### Pending Todos

None yet.

### Blockers/Concerns

- Phase 6 (SEC-04 SSRF guard): the operator's IdP metadata-URL fetch/validation path needs explicit design during Phase 6 planning — is it admission-time CEL only (https + not-link-local, no actual network fetch) or does the operator itself perform a fetch? Research flagged this as a well-precedented CVE class; resolve explicitly before implementation, not by convention.
- `shibd` reload-vs-restart classification remains an open empirical question (DESIGN §11) — Phase 2 defaults to "always roll, gated by config-hash" per architecture research; revisit as a v1.x optimization, not a v1 blocker.
- NetworkPolicy enforcement (Phase 2, SEC-01) is CNI-specific — "the YAML exists" is not "the control is enforced"; Phase 2 planning should include an actual in-cluster verification test against this cluster's CNI (Calico), not just manifest generation.

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none — first milestone)* | | | |

## Session Continuity

Last session: 2026-07-11T12:45:15.233Z
Stopped at: Phase 1 context gathered (assumptions mode)
Resume file: .planning/phases/01-shared-render-aggregation-package/01-CONTEXT.md
</content>
