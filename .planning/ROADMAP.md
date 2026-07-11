# Roadmap: SAML SP Operator

## Overview

The de-risking spike proved the entire config surface by hand (browser → mocksaml
→ Traefik ForwardAuth → SP → whoami, with identity attributes flowing). The
operator's job is to generate, byte-for-byte, what the spike proved works — and
the build order is dependency-ordered, not feature-ordered, so each phase is
independently verifiable against the live spike image before the next adds
complexity. Phase 1 proves the rendering/collision logic in pure Go with zero
cluster dependency. Phases 2–3 stand up each controller alone — SPInstance
first (the riskier rollout mechanics), then AppIntegration in resolution-only
mode (no side effects yet). Phase 4 is the first point two apps can actually
collide with each other. Phase 5 is the true end-to-end integration point:
add one `AppIntegration`, the app is protected. Phase 6 closes out the
production-hardening items that only make sense once the whole system exists
(SSRF guard, full-lifecycle Events, a real least-privilege RBAC audit). Every
phase builds ON the existing `gsd/operator-scaffold` branch (kubebuilder
scaffold, both CRDs, empty reconcilers, envtest green) — no phase re-scaffolds.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Shared Render & Aggregation Package** - Pure Go config-rendering + RequestMap-collision logic, unit-tested against spike fixtures, zero k8s dependency
- [ ] **Phase 2: SPInstance Controller — Static Path & Production Foundations** - A single SPInstance becomes a real, hardened, running SP Deployment (rollout, readiness, sessions, RBAC, NetworkPolicy, leader election) with no AppIntegration awareness yet
- [ ] **Phase 3: AppIntegration Controller — Resolution Only** - An AppIntegration resolves its HTTPRoute and validates SPInstance consent, with no side effects on the SP
- [ ] **Phase 4: Cross-Namespace Aggregation (SPInstance Side)** - The SPInstance controller aggregates real, multi-app AppIntegrations across namespaces — the first point two apps can collide
- [ ] **Phase 5: AppIntegration Middleware Emission, Conflict & Finalizer** - The end-to-end integration point: add one AppIntegration, the app becomes SAML-protected, with correct conflict resolution and clean cross-namespace teardown
- [ ] **Phase 6: Hardening & Observability Closeout** - System-wide production hardening: SSRF guard on metadata fetch, full-lifecycle Kubernetes Events, verified least-privilege RBAC

## Phase Details

### Phase 1: Shared Render & Aggregation Package

**Goal**: The config-rendering and RequestMap-collision logic is proven correct in isolation, before any controller exists — a pure Go package that turns CRD specs into byte-correct, injection-safe Shibboleth SP config. This is the highest-leverage structural decision in the system: both controllers will later import this same package so they can never disagree about a collision winner.
**Depends on**: Nothing (first phase; builds on the CRD types already scaffolded on `gsd/operator-scaffold`)
**Requirements**: RENDER-01, RENDER-02, RENDER-03, RENDER-04, RENDER-05, RENDER-06, RENDER-07, RENDER-08, RENDER-09, RENDER-10
**Success Criteria** (what must be TRUE):

  1. Given a sample `SPInstance` spec, the render package produces a `shibboleth2.xml` that a real containerized `shibd` parses and loads successfully (not just a golden-file text-compare) — includes ApplicationDefaults, Sessions, MetadataProvider, and credentials, plus the `SHIBSP_SERVER_*`/`handlerURL` self-URL derivation
  2. Given two `AppIntegration` fixtures with a colliding `(hostname, path)`, the aggregation package deterministically picks the oldest-`createdAt` winner every time regardless of input map iteration order, and the loser is excluded from the render and distinctly flagged
  3. Every rendered RequestMap `<Host>` carries explicit `scheme`+`port` even on default ports, and hostile CRD string fields (embedded `--`, `<`, `&`) never produce invalid or FATAL-ing XML
  4. The package computes a stable `sha256` hash over rendered bytes that changes if and only if semantic content changes (key-reordering alone doesn't perturb it)
  5. `attribute-map.xml` renders correctly from `AppIntegration.attributes`, and the edge header-hygiene clear-list renders correctly for both the Traefik (enumerate-clear) and nginx (`Variable-*` glob) attachment models

**Plans**: 1/7 plans executed
**Wave 1**

- [x] 01-01-PLAN.md — Types, self-URL derivation, collision resolution (the shared `Resolve` seam) [RENDER-02, RENDER-06] (wave 1)
- [ ] 01-02-PLAN.md — XML self-closing/prolog formatting + config hash primitives [RENDER-09] (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*

- [ ] 01-03-PLAN.md — shibboleth2.xml renderer + RequestMap aggregation + explicit scheme/port [RENDER-01, RENDER-04, RENDER-05] (wave 2)
- [ ] 01-04-PLAN.md — attribute-map.xml renderer [RENDER-03] (wave 2)
- [ ] 01-05-PLAN.md — nginx.conf renderer + edge header clear-list [RENDER-07, RENDER-08] (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [ ] 01-06-PLAN.md — injection safety + pipeline determinism/hash stability [RENDER-10, RENDER-09] (wave 3)
- [ ] 01-07-PLAN.md — gated real-shibd container load test + GHCR public-visibility checkpoint [RENDER-01] (wave 3)

### Phase 2: SPInstance Controller — Static Path & Production Foundations

**Goal**: A single `SPInstance` becomes a real, running, production-hardened SP Deployment in the auth namespace — config-hash-gated rollout, real readiness, memcached sessions, least-privilege secret access, network-isolated, leader-elected — before any `AppIntegration` exists. This phase deliberately front-loads the operator-class production foundations (leader election, RBAC/cache scoping, metrics, NetworkPolicy, CEL validation) alongside the first controller, since they're cheapest to wire correctly now and every later phase inherits them.
**Depends on**: Phase 1
**Requirements**: SPI-01, SPI-02, SPI-03, SPI-05, SPI-07, OBS-03, OBS-05, SEC-01, SEC-02, SEC-03, OPS-01
**Cross-cutting wiring**: Leader election (OPS-01) and base Prometheus metrics (OBS-05, controller-runtime defaults + reconcile/render/rollout counters) wired with this first controller. Secret RBAC/informer-cache scoped to the auth namespace only (SEC-02) from this phase forward. NetworkPolicy (SEC-01) generated as an owned resource alongside the Deployment/Service/ConfigMap. CRD CEL validation (SEC-03) added for both CRDs at this point — reject malformed specs at admission before any controller work is exercised against them.
**Success Criteria** (what must be TRUE):

  1. Applying an `SPInstance` CR against the live cluster produces a running Deployment + ClusterIP Service + headless Service + ConfigMap, and status shows a `Ready` condition plus the generated SP metadata URL
  2. Editing an unrelated field that doesn't change rendered config does not roll the Deployment; editing the entityID or credentials does roll it, gated by the pod-template config-hash annotation
  3. Killing `shibd` inside the pod (or feeding a spec that produces invalid config) flips the readiness probe to NotReady — with `RollingUpdate maxUnavailable: 0` (SPI-07) the rollout halts and the old pod keeps serving; a broken config can never replace a healthy pod
  4. A NetworkPolicy is generated restricting the authenticator Service to gateway-namespace ingress only; a pod-to-pod hit from an unrelated namespace is refused
  5. An `SPInstance` with an invalid spec (malformed external URL, missing credentials, non-https or link-local metadata URL) is rejected at admission by CRD validation, never reaching the reconciler
  6. With `sessionStore` set, the SP Deployment is wired to memcached; running two controller replicas shows only one performing writes (leader election active)

**Plans**: TBD

### Phase 3: AppIntegration Controller — Resolution Only

**Goal**: An `AppIntegration` correctly resolves its target `HTTPRoute` and validates its `SPInstance` consent, and reports accurate status — with zero side effects on the SP yet. Independently verifiable in isolation before it touches the shared render pipeline.
**Depends on**: Phase 2
**Requirements**: APP-01, APP-02
**Cross-cutting wiring**: Establishes the `AppIntegration` status-condition + `observedGeneration` pattern (partial set: `SPInstanceResolved`/`RouteResolved`/`Degraded`) that Phase 5 extends with `Conflict`/`Ready`. Field indexer registration pattern (`spec.targetRef.name`) introduced here, reused by Phase 4/5.
**Success Criteria** (what must be TRUE):

  1. An `AppIntegration` targeting a real `HTTPRoute` resolves hostnames/paths and sets `RouteResolved=True`; a route with only header/method-based matches sets `Degraded` (not silently ignored, since that's not RequestMap-derivable)
  2. An `AppIntegration` whose `SPInstance.allowedNamespaces` excludes its own namespace is rejected (`SPInstanceResolved=False`), never silently bound
  3. An `AppIntegration` pointing at a nonexistent `HTTPRoute` or `SPInstance` reports an accurate, human-readable status condition, and `observedGeneration` matches the object's generation after every reconcile

**Plans**: TBD

### Phase 4: Cross-Namespace Aggregation (SPInstance Side)

**Goal**: The `SPInstance` controller aggregates every consented `AppIntegration` across namespaces into one real, multi-app RequestMap — the first point a second app can actually collide with the first. Consent is re-validated centrally here (never trusted from `AppIntegration.status`), which is the security-relevant heart of this phase.
**Depends on**: Phase 3
**Requirements**: SPI-04, OBS-02
**Cross-cutting wiring**: `SPInstance` status (OBS-02) becomes fully meaningful here — `bound-count` only has real content once cross-namespace aggregation exists; `ConfigRendered`/rollout-health established in Phase 2 now report on a real multi-app render.
**Success Criteria** (what must be TRUE):

  1. Binding a second `AppIntegration` (different namespace) to the same `SPInstance` causes the SP's rendered RequestMap and running config to include both apps' host/path rules, without a manual trigger
  2. `SPInstance` status `bound-count` accurately reflects the number of currently-consented, live `AppIntegration`s, increasing and decreasing as they're added and removed
  3. An `AppIntegration` in a namespace NOT covered by `allowedNamespaces` is excluded from the render even if its own status claims `SPInstanceResolved=True` — the `SPInstance` controller re-derives host/path from the live `HTTPRoute` and re-validates consent against live `Namespace` labels itself
  4. Deleting an `AppIntegration` removes its entry from the live RequestMap on the next reconcile

**Plans**: TBD

### Phase 5: AppIntegration Middleware Emission, Conflict & Finalizer

**Goal**: The true end-to-end integration point — an app team adds one `AppIntegration` CRD and their app becomes SAML-protected, with correct conflict resolution and clean cross-namespace teardown. Depends on every prior phase being correct.
**Depends on**: Phase 4
**Requirements**: APP-03, APP-04, APP-05, OBS-01
**Cross-cutting wiring**: Completes the `AppIntegration` status-condition set from Phase 3 — `Conflict` (self-computed via the shared Phase 1 collision function over the sibling list, no cross-controller status write) and `Ready` (OBS-01) now become real and verifiable end-to-end.
**Success Criteria** (what must be TRUE):

  1. Adding an `AppIntegration` (with an `ExtensionRef` on its `HTTPRoute` pointing at the emitted Middleware) results in a real, working forward-auth round trip: an unauthenticated request bounces to the IdP, an authenticated request reaches the app carrying its mapped identity headers
  2. When two `AppIntegration`s claim the same `(hostname, path)`, the older one gets its Middleware and `Ready`; the younger independently computes and reports its own `Conflict=True` — with neither controller writing into the other's status
  3. Deleting an `AppIntegration` triggers the finalizer: the `SPInstance` re-renders without it before the object is actually removed from the API server
  4. `AppIntegration` status shows all five conditions (`SPInstanceResolved`/`RouteResolved`/`Conflict`/`Degraded`/`Ready`) with correct `observedGeneration` through a full create → conflict → resolve → delete lifecycle

**Plans**: TBD

### Phase 6: Hardening & Observability Closeout

**Goal**: The operator is verifiably production-hardened end-to-end — every significant reconcile transition is observable, the metadata-fetch surface is safe against SSRF, and RBAC is proven least-privilege across the whole system. These are deliberately held until every controller and transition type exists, since each is only meaningfully verifiable at full-system scope.
**Depends on**: Phase 5
**Requirements**: SEC-04, OPS-02, OBS-04
**Cross-cutting wiring**: Kubernetes Events (OBS-04) — the recorder mechanism exists since Phase 2 (rendered/rolled), extended in Phase 3 (degraded) and Phase 5 (conflict); this phase verifies full-lifecycle coverage. RBAC (OPS-02) is generated incrementally from `+kubebuilder:rbac` markers as each phase's controller code lands; this phase is the audited, reviewed checkpoint (CI drift check) confirming no marker outlives its actual usage.
**Success Criteria** (what must be TRUE):

  1. Configuring an `SPInstance` with a non-https or link-local/private IdP metadata URL is rejected before any fetch is attempted — verified against an actual link-local target, not just documented
  2. `kubectl describe`/`get events` on an `SPInstance` or `AppIntegration` shows a Kubernetes Event for every significant transition across the system's lifetime (rendered, rolled, conflict, degraded)
  3. The generated `ClusterRole`/`Role` grants exactly the verbs/resources the two reconcilers actually touch — no wildcard verbs, no untouched resource types — verified against a marker-to-usage audit

**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Shared Render & Aggregation Package | 1/7 | In Progress|  |
| 2. SPInstance Controller — Static Path & Production Foundations | 0/TBD | Not started | - |
| 3. AppIntegration Controller — Resolution Only | 0/TBD | Not started | - |
| 4. Cross-Namespace Aggregation (SPInstance Side) | 0/TBD | Not started | - |
| 5. AppIntegration Middleware Emission, Conflict & Finalizer | 0/TBD | Not started | - |
| 6. Hardening & Observability Closeout | 0/TBD | Not started | - |
</content>
