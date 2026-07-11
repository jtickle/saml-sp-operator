# Project Research Summary

**Project:** SAML SP Operator
**Domain:** Kubernetes operator (Go/kubebuilder/controller-runtime) — two-CRD, cross-namespace reconciler wrapping a containerized Shibboleth SP v3 as a gateway-portable forward-auth authenticator
**Researched:** 2026-07-09/10
**Confidence:** HIGH (stack, architecture) / MEDIUM (features, pitfalls)

## Executive Summary

This is a production-grade Kubernetes operator problem sitting on top of an already-proven config surface: the de-risking spike validated the entire Shibboleth SP + Traefik ForwardAuth round-trip end-to-end, including six hardest-won lessons (RequestMap fail-open on non-standard ports, `SHIBSP_SERVER_*` env vs. fastcgi_params, headless-Service-not-ClusterIP for ForwardAuth, unbounded `Variable-*` header leakage under Traefik, dumb readiness masking `shibd` FATAL, and illegal `--` in XML comments FATALing the parser). None of that is still a research question — it's a checklist the operator's renderer and rollout logic must not regress. The scaffold (`gsd/operator-scaffold`) is already on current dependency versions; the only new stack surface is `encoding/xml`-based structural rendering for `shibboleth2.xml`/`attribute-map.xml`, `text/template` for the line-oriented `nginx.conf`, stdlib `crypto/sha256` for config-hash rollout gating, `gateway-api` v1.6.0 for read-only `HTTPRoute` introspection, and a small hand-rolled Traefik `Middleware` types package.

The recommended build order — proposed by the architecture research and adopted here as the primary roadmap skeleton — is dependency-ordered, not feature-ordered: build the pure render/collision algorithm first (zero k8s dependency), then bring up a static-path `SPInstance` controller against the live cluster, then an `AppIntegration` controller that only resolves (no side effects), then wire `SPInstance`-side cross-namespace aggregation, and only in the final phase let `AppIntegration` emit its Middleware, compute its own `Conflict`, and hold the cross-namespace finalizer. This ordering front-loads the two riskiest things — collision-ordering correctness and the live rollout mechanics — before cross-namespace complexity makes debugging harder.

The biggest risk is not the SAML/crypto layer (deliberately borrowed, not built) but scope creep vs. silent gaps in "production operator table stakes": PROJECT.md's Active list is strong on SAML-specific behavior but currently omits leader election, Prometheus metrics, Kubernetes Events, CRD CEL validation, NetworkPolicy generation for the authenticator Service, and an explicit memcached-Sessions rendering target and federation-metadata-refresh strategy. NetworkPolicy generation is what makes the `X-Forwarded-Host`-based authorization model actually trustworthy rather than merely documented as a constraint, and memcached-Sessions rendering is what makes "rolling restart is safe" true rather than assumed. These should be pulled into the roadmap explicitly.

## Key Findings

### Recommended Stack

The scaffold's core dependencies (controller-runtime v0.24.1, apimachinery v0.36.0, kubebuilder v4.15) are already current — no version debt beyond a routine ginkgo/gomega minor bump. All new implementation surface is stdlib or already-present controller-runtime/apimachinery helpers.

**Core technologies:**
- `encoding/xml` (stdlib) — render `shibboleth2.xml`/`attribute-map.xml` as typed Go structs — the RequestMap is a tree with strict ordering rules a text DSL fakes badly, and struct marshaling gets XML-escaping for free (closes the injection-shaped risk).
- `text/template` (stdlib) — render `nginx.conf`; keep the func map to 2-3 hand-written helpers, no `sprig`.
- `crypto/sha256` + `encoding/hex` (stdlib) — hash rendered config bytes, stamp as a pod-template annotation for config-hash-gated rollout; avoid `k8s.io/kubectl`'s `DeepHashObject`.
- `sigs.k8s.io/gateway-api` v1.6.0 — read-only `HTTPRoute` types (`apis/v1` only, GA).
- Hand-rolled local Traefik `Middleware`/`ForwardAuth`/`Headers` types (`internal/traefik/v1alpha1`) — importing `traefik/traefik` drags in ~95 unrelated dependencies for ~4 fields.
- controller-runtime helpers already available: `meta.SetStatusCondition` family, `controllerutil.AddFinalizer`/`RemoveFinalizer`/`ContainsFinalizer`, current `Watches(client.Object, ...)`/`Owns()` builder API, `mgr.GetFieldIndexer().IndexField`, `controllerutil.CreateOrUpdate`.
- Testing: golden-file pattern plus, non-negotiably, a real containerized `shibd` parse/startup check as a CI gate — golden-file text-compare cannot catch config that's well-formed-looking but semantically wrong (the `--`-in-comment bug regressed twice in the spike).

### Expected Features

PROJECT.md's 13 Active items are strong on SAML-specific behavior but thin on generic production-operator table stakes. Treat the gaps below as launch-blocking, since several gate the "production-grade" claim.

**Must have — already Active, keep:** SP metadata URL in status; `shibboleth2.xml` rendering; RequestMap collision handling (deterministic winner); edge header hygiene/clear-list; real SP readiness probe; config-hash-gated rollout; `allowedNamespaces` consent validation; cross-namespace finalizers.

**Must have — gaps to add to Active/roadmap:**
- `observedGeneration` explicit on every status condition (bake into the shared condition-setting helper from the first reconciler)
- Leader election (cheap now, expensive once multi-replica is assumed)
- Least-privilege RBAC tracked as a reviewed deliverable (CI drift check)
- Basic Prometheus metrics (controller-runtime defaults + bound-AppIntegration-count, conflict-count)
- Kubernetes Events on every status-condition transition, not just `Conflict`
- CRD CEL validation for invariants without cluster-state needs (entityID well-formedness, non-empty `allowedNamespaces`, no duplicate attribute→header mappings)
- Memcached `Sessions`/`StorageService` rendering as its own explicit, independently-tested target — the "rolling restart is safe" claim depends on this being correct
- NetworkPolicy generation (or an explicit, loud, documented deferral) scoping the authenticator Service to gateway-namespace-only ingress
- An explicit, chosen federation-metadata-refresh strategy for egress-restricted clusters

**Should have (v1.x):** Config-reload-vs-pod-roll optimization; well-known `/__auth/*` contract; ReferenceGrant generation (once non-Traefik attachment lands); opt-in hostname-claim VAP.

**Defer (v2+):** App-queryable session API, force-logout/audit index, GEP-1494/Envoy dataplane support, cross-host centralized ACS, OLM/OperatorHub packaging.

### Architecture Approach

Two controllers, split by namespace and lifecycle, sharing zero write access to each other's objects but sharing one pure, k8s-independent Go package (`internal/requestmap` + `internal/render`) for the collision algorithm and config templating — this is the highest-leverage structural decision, since it's the trickiest logic in the system and the only thing both controllers must never disagree about. Field indexes on `.spec.spInstanceRef` and `.spec.targetRef.name` make cross-namespace fan-out O(1). Cross-namespace coordination is via finalizers, never ownerRefs (silently namespace-local, never cascade GC across a boundary). Consent (`allowedNamespaces`) is re-validated centrally by the `SPInstance` controller on every aggregation pass — never trusted from `AppIntegration.status`.

**Major components:**
1. `SPInstanceReconciler` (auth namespace) — single writer of SP Deployment/ConfigMap/Service/headless-Service and the aggregate RequestMap; drives config-hash-gated rollout.
2. `AppIntegrationReconciler` (app namespaces) — single writer of its own Middleware and status; resolves HTTPRoute, re-validates SPInstance consent, independently computes its own collision outcome.
3. `internal/requestmap` / `internal/render` — pure Go, zero `client.Client` dependency, imported by both reconcilers.
4. Field indexers (`internal/controller/index.go`) — centralized registration, called once from `main.go` before either controller starts.

### Critical Pitfalls

Six are spike-proven and MUST-CARRY-FORWARD:

1. **RequestMap `<Host>` without explicit scheme+port fails OPEN** — always emit explicit scheme/port, even on 443. Mandatory negative test: unauthenticated hit to a protected non-standard-port route must return non-200.
2. **`SHIBSP_SERVER_*` process env, not fastcgi_params, drives SP self-URL scheme** — render as Deployment `env:`, unit test consistency with `handlerURL`.
3. **ForwardAuth must target a headless Service, not the ClusterIP** — CNI/kube-proxy-specific dial failure; e2e assertion the emitted address is the headless FQDN.
4. **Unlisted `Variable-*` headers ride through Traefik ForwardAuth** — enumerate-clear known control headers, document the residual gap as a hard security contract.
5. **Dumb readiness probes report Ready while `shibd` is FATAL** — real health check proving `shibd` loaded; chaos test must flip readiness.
6. **Illegal `--` in XML comments FATALs `shibd`** — never hand-author freeform comments into rendered config; validate rendered XML before writing the ConfigMap.

Plus new operator-class pitfalls: missing field indexers forcing O(n) scans, non-idempotent reconcile hot-looping from unconditional status writes, Go map iteration non-determinism defeating config-hash-gated rollout, XML injection via naive `text/template` on CRD string fields, and controller-runtime's default cluster-wide Secret cache turning operator compromise into a cluster-wide secret-read primitive unless scoped to the auth namespace.

## Implications for Roadmap

The architecture research's five-phase build order is the strongest, most concrete input available and is adopted directly as the roadmap skeleton, front-loading the riskiest new logic before cross-namespace complexity compounds debugging difficulty.

### Phase 1: Shared Render & Aggregation Package
**Rationale:** Nothing downstream can be correctly tested until this exists, and it needs no cluster to validate.
**Delivers:** Pure Go `internal/requestmap` (host/path partition, deterministic collision ordering, nested-path construction) and `internal/render` (shibboleth2.xml/nginx.conf/attribute-map.xml templating, config hashing), unit-tested against spike fixtures. Zero controller-runtime dependency.
**Addresses:** RequestMap collision handling, config-hash-gated rollout (hashing mechanism).
**Avoids:** Pitfall 1 (fail-open), Pitfall 6 (illegal `--`), Pitfall 10 (nondeterministic serialization), Pitfall 11 (XML injection).

### Phase 2: SPInstance Reconciler — Static Path
**Rationale:** Reproduces "bring up a bare authenticator" against the live cluster before cross-namespace fan-out complicates debugging.
**Delivers:** `For(&SPInstance{})` + `Owns(Deployment/ConfigMap/Service/headless-Service)`; renders config from `SPInstance` spec alone with an empty RequestMap; config-hash-gated rollout end-to-end; real SP readiness probe; Secret watch for credential rotation.
**Uses:** `encoding/xml` render pipeline (Phase 1), `crypto/sha256` hashing, controller-runtime `Owns()`/status-condition helpers.
**Implements:** `SPInstanceReconciler`; scope Secret RBAC/cache to auth namespace only from this phase.
**Avoids:** Pitfall 2 (`SHIBSP_SERVER_*` env), Pitfall 5 (dumb readiness).

### Phase 3: AppIntegration Reconciler — Resolution Only
**Rationale:** Independently verifiable without yet touching the render pipeline.
**Delivers:** `For(&AppIntegration{})`, same-namespace `Watches(HTTPRoute)`, cross-namespace `Watches(SPInstance)`; resolves route, first-pass consent check, sets `SPInstanceResolved`/`RouteResolved`/`Degraded` and `resolvedHostnames` status. No `Conflict`, no Middleware yet.
**Implements:** `AppIntegrationReconciler` skeleton; field indexer registration pattern.
**Avoids:** Pitfall 9 (missing field indexers).

### Phase 4: Cross-Namespace Aggregation (SPInstance Side)
**Rationale:** Depends on Phases 1-3; first point a second app can actually collide with the first.
**Delivers:** `spec.spInstanceRef` composite-key field index; `SPInstanceReconciler` gains `Watches(AppIntegration)` + two-hop `Watches(HTTPRoute)`; central re-validation of consent against live `Namespace` labels; real multi-app RequestMap through Phase 1's algorithm into Phase 2's render pipeline.
**Addresses:** `allowedNamespaces` consent validation (centralized, authoritative side).
**Avoids:** trusting derived status across a controller boundary.

### Phase 5: AppIntegration Middleware Emission, Conflict, and Finalizer
**Rationale:** The true end-to-end integration point ("add one CRD, app becomes protected") — depends on every prior phase.
**Delivers:** `Owns(Middleware)` same-namespace; self-determined `Conflict` via the shared collision function over sibling list; `authResponseHeaders` + per-model header-hygiene clear-list; the `saml.tickletechnologies.com/appintegration-cleanup` finalizer (simple fire-and-remove is safe for Traefik-only v1).
**Addresses:** Edge header hygiene/clear-list (Active requirement).
**Avoids:** Pitfall 4 (unlisted `Variable-*` leak), Pitfall 7 (cross-namespace ownerRef GC trap), two controllers writing the same object.

### Cross-Cutting: Fold Into the Above Phases, Not a Separate Phase

The feature-research gaps aren't a standalone phase — they're cross-cutting concerns threaded through Phases 2-5 as acceptance criteria:
- `observedGeneration` + shared condition helper: Phase 2 (first reconciler).
- Leader election + least-privilege RBAC tracking: Phase 2 scaffolding.
- Memcached Sessions/StorageService rendering: explicit acceptance criterion in Phase 2.
- NetworkPolicy generation for the authenticator Service: Phase 5 (Gateway-attachment phase).
- Federation-metadata-refresh strategy and CRD CEL validation: decide explicitly during Phase 2/3 planning.
- Prometheus metrics + Kubernetes Events: incremental additions across Phases 2-5.

### Phase Ordering Rationale

- Dependency order, not feature order: pure algorithm → static single-controller mechanics → resolution-only second controller → cross-namespace aggregation → full integration.
- Front-loads the two things most likely to have subtle, hard-to-debug bugs (collision determinism, rollout mechanics) while the system is still simple enough to isolate failures.
- Security-relevant re-validation (consent, RequestMap) is deliberately placed on the centralized/auth-namespace side in every phase where it appears.
- Deferred explicitly past this build (not blocking v1): `ReferenceGrant` lifecycle, config reload-vs-roll optimization, the confirm-then-remove finalizer handshake.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 5 (Middleware emission + NetworkPolicy):** header-hygiene clear-list and NetworkPolicy-enforcement-per-CNI are still empirically underspecified per-cluster.
- **A later phase (federation metadata mirror, likely post-v1):** SSRF-hardening for any operator-implemented metadata-fetch path is a well-precedented CVE class needing its own design pass.
- **Rollout & status phase (folded into Phase 2, follow-up in Phase 5):** `shibd` reload-vs-restart classification is explicitly an open empirical question — needs a small targeted validation spike.

Phases with standard patterns (skip research-phase):
- **Phase 1:** pure Go, well-understood idioms — no external unknowns.
- **Phase 3:** standard controller-runtime resolve-and-set-status pattern, directly documented by verified API signatures.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions verified against pkg.go.dev and raw GitHub `go.mod` files; the one MEDIUM judgment call (hand-roll vs. import Traefik types) is flagged explicitly with a stated fallback |
| Features | MEDIUM | Websearch-sourced, cross-checked across multiple independent snippets; strong internal grounding from DESIGN.md/PROJECT.md for the SAML-specific half |
| Architecture | HIGH (API surface) / MEDIUM (specific recommendations) | controller-runtime signatures verified against the exact pinned v0.24.1; finalizer-depth/single-writer resolutions are this research's own synthesis of ambiguity DESIGN.md left open |
| Pitfalls | HIGH (6 spike-proven) / MEDIUM (10 new operator pitfalls) | Spike-proven items are first-party verified; general pitfalls are well-established community sources cross-checked across multiple; the SSRF item is MEDIUM-HIGH, backed by a named CVE/GHSA advisory in a comparable codepath |

**Overall confidence:** MEDIUM-HIGH — the hardest, most bypass-prone part of the system (SAML/crypto) is out of scope by design and already spike-proven; remaining uncertainty is concentrated in generic-operator-scale questions (RBAC/cache scoping, NetworkPolicy enforcement per-CNI) and a small number of explicitly-flagged open empirical questions inherited from DESIGN.md §11.

### Gaps to Address

- **`shibd` reload-vs-restart boundary is unproven:** treat as a small, explicit empirical validation step inside the rollout phase; default to "always roll, gated by config-hash" until proven otherwise.
- **NetworkPolicy enforcement varies by CNI:** "the YAML exists" is not "the control is enforced" — needs an actual in-cluster verification test per target CNI.
- **Federation-metadata-refresh strategy for egress-restricted clusters is undecided:** needs one explicit chosen answer before any customer cluster with no general egress is targeted; if an in-cluster mirror is built, SSRF-harden it as a hard requirement of that phase.
- **Traefik hand-rolled types vs. official import is a judgment call, not a verified best practice:** cross-check field names against Traefik's official Middleware CRD reference docs before locking in.
- **The finalizer-depth and single-writer resolutions in the architecture research are opinionated synthesis, not external source fact:** treat as adoptable defaults, open to explicit override during planning discussion.

## Sources

### Primary (HIGH confidence)
- pkg.go.dev — `sigs.k8s.io/controller-runtime` v0.24.1, `sigs.k8s.io/gateway-api` v1.6.0, `github.com/onsi/ginkgo/v2`, `github.com/onsi/gomega`, `github.com/google/go-cmp` version/API surfaces
- `github.com/traefik/traefik` `go.mod` (raw GitHub) — direct dependency-count measurement
- `kubernetes-sigs/gateway-api` `go.mod` (raw GitHub) — apimachinery/client-go compatibility
- `kubernetes-sigs/controller-runtime` GitHub issues #687, #1330, #1941 — cross-namespace watch and field-indexer maintainer guidance
- `DESIGN.md` and `.planning/threads/saml-sp-operator.md` (this repo) — six MUST-CARRY-FORWARD spike-proven pitfalls, first-party
- `.planning/PROJECT.md` — Active requirements, Constraints, Key Decisions

### Secondary (MEDIUM confidence)
- The Kubebuilder Book (Good Practices, RBAC markers, metrics reference)
- Operator SDK / operatorframework.io — Operator Capability Levels, best practices, observability
- Ahmet Alp Balkan, "So you wanna write Kubernetes controllers?" — controller pitfalls
- Shibboleth SP3/IdP4 wikis — `ReloadableConfiguration`, `FileBackedHTTPMetadataProvider` behavior
- GEP-1494 spec, Envoy Gateway ext-auth docs — swappable-attachment portability context

### Tertiary (LOW-MEDIUM confidence)
- openedx-platform GHSA-64cv-vxpr-j6vc / GHSA-328g-7h4g-r2m9 — SSRF precedent in a comparable SAML-metadata-fetch codepath
- General web search on `text/template` vs `html/template` escaping guidance

---
*Research completed: 2026-07-10*
*Ready for roadmap: yes*
