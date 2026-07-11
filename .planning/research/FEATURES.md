# Feature Research

**Domain:** Kubernetes operator reconciling CRDs (`SPInstance`, `AppIntegration`) into a running Shibboleth SP forward-auth authenticator
**Researched:** 2026-07-10
**Confidence:** MEDIUM (websearch-sourced, cross-checked against multiple independent snippets per topic; no single-source claims treated as authoritative)

## Feature Landscape

This is deliberately organized as two intersecting sets: **generic production-K8s-operator table stakes** (true for any kubebuilder/controller-runtime operator) and **SAML-SP-operator-specific table stakes** (true because the workload is Shibboleth SP behind forward-auth). PROJECT.md's Active list is strong on the SAML-specific half and thin on the generic-operator half — see the Gaps section below for exact deltas.

### Table Stakes — Generic Kubernetes Operator (Users/Ops Expect These)

Missing any of these makes the operator feel like a hobby project, not something you'd put in front of an audit (the same SOC2/PCI/ISO pressure that killed ingress-nginx in this project's own history — DESIGN §1).

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Status **Conditions** with `observedGeneration` set on every condition | GitOps tooling gates "rollout complete" on `observedGeneration == metadata.generation`; omitting it is the single most common cause of "pipeline says green, prod is stale" on operator-managed resources | LOW | PROJECT.md Active already lists the condition *types* (`SPInstanceResolved`/`RouteResolved`/`Conflict`/`Degraded`/`Ready`, `ConfigRendered`) but never says `observedGeneration` explicitly — bake it into the condition-setting helper from the first reconciler, not bolted on later |
| **Finalizers** for cross-namespace cleanup | Already correctly identified (DESIGN §7 ownerRef trap) — a namespaced object can't own a resource in another namespace, so GC silently won't fire without one | MEDIUM | Active. Must be idempotent and must always eventually remove itself; ship a documented force-removal runbook (`kubectl patch ... --type merge -p '{"metadata":{"finalizers":[]}}'`) for the day it doesn't |
| **Leader election** | Any HA deployment (>1 operator replica) needs exactly one active reconciler or you get duplicate/conflicting RequestMap renders and racing rollouts | LOW | **Missing from Active entirely.** controller-runtime/kubebuilder wires this with one manager flag; needs RBAC on `coordination.k8s.io` leases (create/get/update). Cheap to add now, expensive to retrofit once multi-replica is assumed elsewhere |
| **Least-privilege RBAC** generated from kubebuilder markers | Scaffold already generates RBAC from `//+kubebuilder:rbac` markers per controller, but this needs to be a tracked deliverable (audited as the operator grows), not an artifact nobody reviews | LOW | Add a CI check that generated `config/rbac/role.yaml` has no drift from markers, and periodically audit that verbs/resources still match what's actually touched |
| **Prometheus metrics** | Fastest signal for whether the reconciler is healthy: falling behind, erroring, hot-looping, being throttled by the API server | MEDIUM | **Missing from Active.** controller-runtime auto-exposes reconcile latency/errors, workqueue depth/retries, REST client rate/429s, leader-election status for free via `pkg/metrics.Registry`. Add business metrics (bound-`AppIntegration`-count, rollout-in-progress, `Conflict` count) the same way. Keep labels bounded (`controller`, `namespace`, `kind`, `result`, `reason`) — never label by CR name/UID |
| **Kubernetes Events** (Recorder) on state transitions | Complements conditions/metrics with per-object human-readable detail ops actually reads in `kubectl describe` | LOW | DESIGN §7 mentions "loser gets a `Conflict` condition **+ Event**" but this isn't systematized across all transitions in Active. Emit an Event on every condition transition (rollout started, config rendered, conflict detected, degraded) |
| **Idempotent, side-effect-free reconcile** | The controller-runtime contract — reconcile can be called any number of times for any reason and must converge to the same state | LOW (discipline, not code) | Implicit in "config-hash-gated rollout" (Active) but worth stating as an explicit test requirement: re-running Reconcile with no spec change must be a no-op (no Event spam, no Deployment touch) |
| **Drift correction** | If someone hand-edits the generated ConfigMap/Deployment, the operator should notice and revert on next reconcile (own `Owns()`/watch, not just create-once) | LOW–MEDIUM | Not explicit in Active. Falls out naturally from `Owns(Deployment, ConfigMap, ...)` + comparing rendered-vs-observed, but call it out as an acceptance test ("hand-edit the ConfigMap, confirm it's reverted") |
| **CRD OpenAPI schema validation + CEL** (`x-kubernetes-validations`) for cross-field invariants | Free, at-admission validation with no extra pods/certs; stable since K8s 1.29. Candidates here: `allowedNamespaces` non-empty when set, entityID is a well-formed URI, `attributes` mapping has no duplicate header names | LOW–MEDIUM | **Missing from Active entirely** — no validation strategy is mentioned for either CRD. Decide CRD-schema-first; reserve a real ValidatingWebhook only for checks needing cluster state the schema can't see (e.g., "does `spInstanceRef` resolve to an `SPInstance` that actually allows this namespace" — arguably fine to leave as a status `Degraded` condition instead of blocking admission, given the operator already has to handle async resolution) |
| **NetworkPolicy generation/enforcement** for the authenticator Service | DESIGN's own security section says the Service "must be reachable only from the gateway" because `applicationId`/authz depend on client-spoofable `X-Forwarded-Host` — this is a stated *security constraint*, not an *operator behavior* | MEDIUM | **Missing from Active.** Either the operator emits a NetworkPolicy scoping ingress to the gateway namespace, or this is explicitly platform-owned and the operator should at minimum surface a `Degraded`/warning condition when it can detect the policy is absent. Silently trusting the platform to do this is exactly the kind of "cross-cutting concern by convention" gap this project's own engineering defaults warn about |
| **ReferenceGrant generation** for the cross-namespace data-plane `backendRef` | DESIGN §5 names this as needed "in the auth namespace" for attachment types using a real `backendRef` (i.e., anything except Traefik's ForwardAuth-by-URL) | LOW–MEDIUM | Not in Active (Traefik doesn't need it today, but the swappable-attachment design explicitly anticipates GEP-1494/Envoy `backendRef` later — DESIGN §9 addendum). Flag as a companion feature that must land *before* any non-Traefik attachment ships, or it silently breaks on the first Envoy/Cilium cluster |

### Table Stakes — SAML-SP-Operator-Specific

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| SP metadata URL surfaced in `SPInstance` status | So IdP admins can register the SP without spelunking config — explicit success metric of the project | LOW | Active |
| `shibboleth2.xml` rendering (ApplicationDefaults/Sessions/MetadataProvider/credentials) | Core value prop | HIGH | Active |
| **Memcached session-store wiring** rendered into `Sessions`/`StorageService` | Constraints section states "shared memcached for cross-replica sessions" as a hard requirement (rolling restart is only safe because sessions are external — DESIGN §8) but this is never called out as its own Active bullet; it currently hides inside the generic "renders shibboleth2.xml" line | MEDIUM | **Gap.** Should be an explicit, independently-testable rendering target: given an `SPInstance.spec` session-store ref, the operator emits a correct memcached `StorageService` block (host/port/pool), not just "some Sessions config." Also the schema/session model needs the `(NameID, SessionIndex)` secondary indexing DESIGN §8 calls out for SLO — worth a dedicated acceptance check |
| **Federation metadata refresh in egress-restricted clusters** | Explicitly called out in PROJECT.md Constraints ("remote federation metadata can't be fetched without an egress allowance or an in-cluster metadata mirror") | MEDIUM–HIGH | **Gap — this is a Constraint, not an Active behavior.** Shibboleth's own metadata provider (`FileBackedHTTPMetadataProvider`) only uses its local backing file at cold-start, never as a live fallback — so "no egress" isn't solved by the engine alone. The operator needs *some* answer: (a) document/generate the NetworkPolicy egress allowance for the metadata URL, or (b) support a purely local/pre-fetched metadata-file mode (no periodic refresh — accept staleness), or (c) treat an in-cluster metadata mirror/cache as a first-class managed dependency. Silence on this in Active means it'll be discovered live, in the first air-gapped customer cluster, not in design |
| **RequestMap collision handling** (deterministic winner, `Conflict` condition, never last-write-wins) | Already the single most carefully-specified item in Active — good | HIGH | Active |
| **Edge header hygiene / clear-list generation per attachment model** | Spike-proven security finding: Traefik ForwardAuth can only enumerate-clear, never wildcard-strip `Variable-*`; nginx-model can wildcard-clear | MEDIUM | Active. Depends on the attribute-map render (need the full `Variable-*` vocabulary first) |
| Real SP readiness probe (proves `shibd` loaded, not dumb 200) | Spike-proven (fix J); the difference between "pod Ready" and "pod actually authenticating" | MEDIUM | Active |
| Config-hash-gated rollout | Prevents unrelated `AppIntegration` reconciles from churning the whole SP fleet | MEDIUM | Active |
| **`AppIntegration` consent validation** (`allowedNamespaces`) | Prevents a tenant binding to another tenant's federation trust by name | LOW–MEDIUM | Active, but pairs naturally with the CRD-CEL-validation gap above — decide whether consent-check failure is a CRD-admission rejection or a `Degraded` status condition (current design implies the latter, given async cross-ns resolution — worth stating explicitly rather than leaving implicit) |

### Differentiators (Competitive Advantage — Align With Core Value)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Config-reload-instead-of-pod-roll for hot-reloadable `shibboleth2.xml` changes | Faster, less disruptive convergence for routine `AppIntegration` edits (add/remove an app doesn't have to roll the whole SP Deployment) | HIGH | DESIGN §7 flags this as "investigate" — genuinely differentiates from naive "always roll" operators, but needs to first nail down exactly which config sections `shibd` hot-reloads |
| App-queryable session API (with its own authz: an app sees only its own sessions, a user only their own) | Lets app teams build "active sessions" / force-logout UX without touching SAML | MEDIUM–HIGH | DESIGN §8 names this as an optional add-on riding the `(NameID, SessionIndex)`/`user→sessions` secondary index the session schema already needs for SLO — cheap to justify once that index exists |
| Force-logout / audit via the `user → sessions` secondary index | Compliance/security teams get "kill this user's sessions everywhere" for free once the index exists | LOW (given the index) | Same dependency as above |
| Well-known `/__auth/*` contract (`logout`, `whoami`) uniform across all hosts | Apps get one stable contract and never know SAML exists — directly serves the Core Value ("no SAML expertise required") | LOW–MEDIUM | DESIGN §8 names this but it's not in PROJECT.md Active at all — worth promoting into the requirements list since it's directly load-bearing for the stated success metric |
| Swappable Gateway-attachment layer (Traefik Middleware today, GEP-1494 `ExternalAuth`/Envoy `SecurityPolicy` later) with the SP image unchanged | The portability story that's the entire reason this project exists (ingress-nginx EOL forced a migration once already) | HIGH | Correctly deferred per Out of Scope; the 302-relay spike is the gating unknown before this becomes real (DESIGN §9 addendum) |
| Opt-in hostname-claim protection (ValidatingAdmissionPolicy + operator-side byte-identical check) | Defense-in-depth against accidental/hostile `HTTPRoute` hostname collisions at SaaS scale | MEDIUM | DESIGN §9 has a fully-drafted VAP ready to go, but it's absent from PROJECT.md Active — reasonable to treat as v1.x rather than v1 launch-blocking, since it's opt-in and single-team clusters don't need it day one |

### Anti-Features (Explicitly Do NOT Build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| Writing a from-scratch SAML/XML-DSIG core | "We could control everything, no dependency risk" | XML Signature Wrapping, C14N/transform handling, `KeyInfo` trust confusion — a subtle bug is a silent, total, undetectable auth bypass. The one viable Rust crate (`samael`) is 0.0.x and still shells out to C `xmlsec1` anyway | Borrow Shibboleth SP v3 (already decided, DESIGN §2) |
| Per-app entityID / `ApplicationOverride` by default | "Feels more isolated, one app = one identity" | Federation anti-pattern — InCommon-style federations interoperate poorly with per-app entities; multiplies registration burden per app instead of per deployment | One entityID per deployment; differentiate by per-path content settings + AccessControl (DESIGN §5) — already Out of Scope |
| Operator owning/creating the Gateway itself | "Simpler if the operator manages the whole stack" | Couples the operator to a specific gateway implementation and ownership model, breaks the portability goal that's the entire point of this rewrite | Attach routes + Middleware to a platform-provided Gateway (already Out of Scope) |
| Full OLM/OperatorHub packaging (CSV, bundle, catalog source) at v1 | "Looks more official/production-grade" | Real ceremony (CSV authoring, bundle validation, catalog maintenance) with no customer asking for it yet — this is a single-team/self-hosted cluster today, not an OperatorHub distribution play | Plain `kubectl apply`/Helm/Kustomize install; revisit OLM packaging only if/when a marketplace listing is actually a go-to-market lever |
| Mutating webhooks that silently rewrite user-authored CR fields (defaulting-via-mutation beyond simple `spec` defaults) | "Convenient, fills in smart defaults automatically" | Breaks GitOps diffing (the CR in git no longer matches the live object) and hides operator behavior from the user reading their own YAML | CRD schema defaults (`+kubebuilder:default`) for simple cases; status-surfaced computed values (e.g., SP metadata URL) for anything derived, never silently rewritten spec fields |
| Multi-version CRD + conversion webhooks now | "Future-proof the API early" | Real complexity (conversion webhook, storage version migration) for an API that's still `v1alpha1` and actively shaped by an unfinished spike | Stay on a single `v1alpha1` version until the schema stabilizes post-launch; version only when a real breaking change is needed |
| Cross-host centralized ACS / parent-domain cookie now | "Do it right the first time, avoid re-touching the IdP later" | Explicitly deferred (DESIGN §11) — introduces parent-domain cookie risk and a bigger topology change before the single-host model has even shipped once | Ship single-host first (already Out of Scope for v1, correctly) |
| GEP-1494 `ExternalAuth`/Cilium-Envoy dataplane support now | "Be gateway-native everywhere from day one" | The 302-relay behavior (does the dataplane relay the SP's redirect-to-IdP?) is an **unproven portability unknown** — building support before spiking it risks shipping a broken interactive-login path on a second dataplane | Ship Traefik-only, spike the 302-relay question per dataplane before extending (already Out of Scope) |

## Feature Dependencies

```
Status Conditions (with observedGeneration baked in)
    └──must precede──> any reconciler code (retrofitting is expensive)

Attribute-map rendering (Variable-<id> vocabulary)
    └──requires──> Edge header hygiene clear-list generation
                       (can't clear/enumerate what you haven't rendered yet)

Memcached Sessions rendering
    └──enhances──> Config-hash-gated rollout
                       (rolling restart is only safe BECAUSE sessions are external —
                        DESIGN §8; without this, "safe roll" isn't actually true)
    └──requires──> (NameID, SessionIndex) session schema
                       └──enables──> App-queryable session API [differentiator]
                       └──enables──> Force-logout/audit via user→sessions index [differentiator]

RequestMap aggregation
    └──requires──> RequestMap collision handling
                       (aggregation without deterministic collision resolution
                        silently regresses to last-write-wins)

NetworkPolicy generation (Service reachable only from gateway ns)
    └──gates──> "production-grade" claim for the ForwardAuth Middleware feature
                   (without it, applicationId/authz spoofing via X-Forwarded-Host
                    is a live risk, per PROJECT.md Security constraint)

ReferenceGrant generation
    └──gates──> any non-Traefik attachment (GEP-1494/Envoy backendRef)
                   (Traefik's ForwardAuth-by-URL sidesteps this entirely today,
                    which is why its absence from Active hasn't bitten yet)

Federation metadata refresh strategy (egress allowance | local-file mode | in-cluster mirror)
    └──conflicts with──> "no general egress" cluster constraint
                            (must pick ONE explicit answer per SPInstance/cluster;
                             leaving it implicit means it's discovered in production)

Leader election
    └──required-by──> any multi-replica operator Deployment
                          (harmless no-op at replica=1, load-bearing at replica>1)
```

### Dependency Notes

- **Status Conditions must precede reconciler code:** `observedGeneration` and a shared condition-setting helper are cheap to build in from the first line of Reconcile and expensive to retroactively thread through every controller later. Land this before or alongside the first `SPInstance`/`AppIntegration` reconciler, not after.
- **Memcached rendering enhances config-hash rollout:** the entire "rolling restart is safe" claim in DESIGN §8 is *conditional* on sessions actually living in memcached correctly — if the rendered `Sessions`/`StorageService` block is wrong, rollout safety is an illusion. Treat memcached wiring as launch-blocking, not a nice-to-have layered on top.
- **NetworkPolicy generation gates the production-grade claim on ForwardAuth:** PROJECT.md's own Security constraint says the authenticator Service depends on trusting `X-Forwarded-Host`, which is client-spoofable unless only the gateway can reach it. Shipping ForwardAuth wiring (Active) without also shipping (or explicitly deferring with a loud caveat) the NetworkPolicy that makes that trust valid is the kind of "cross-cutting concern by convention" gap the project's own engineering defaults warn about.
- **Federation metadata refresh conflicts with the no-egress constraint:** this needs one explicit, chosen strategy per deployment (not "the engine will figure it out") because Shibboleth's own metadata provider does not fall back to a local file during live refreshes — only at cold start.

## MVP Definition

### Launch With (v1)

Everything already in PROJECT.md Active, **plus** the generic-operator table stakes and SAML-specific gaps this research surfaced as launch-blocking (not nice-to-haves):

- [ ] All 13 items currently in PROJECT.md Active — the config-surface core, spike-proven
- [ ] `observedGeneration` set on every status condition (folded into the existing status-conditions item, not a separate feature — but must be explicit in the plan/acceptance criteria)
- [ ] Leader election enabled (one manager flag + lease RBAC) — cheap now, expensive to retrofit once multi-replica is assumed
- [ ] Least-privilege RBAC tracked as a reviewed deliverable (markers audited, CI drift check), not just scaffold-default
- [ ] Basic Prometheus metrics (controller-runtime defaults + bound-AppIntegration-count, conflict-count) — fastest signal for "is the operator itself healthy"
- [ ] Kubernetes Events on every status-condition transition, not just `Conflict`
- [ ] CRD CEL validation for the invariants that don't need cluster state (entityID well-formedness, non-empty `allowedNamespaces` when set, no duplicate attribute→header mappings)
- [ ] Memcached `Sessions`/`StorageService` rendering as its own explicit, independently-tested rendering target (not implicit inside "renders shibboleth2.xml")
- [ ] An explicit, chosen federation-metadata-refresh strategy for egress-restricted clusters (even if the v1 answer is "document the required NetworkPolicy egress allowance" — the point is it's a decision, not a silent gap)
- [ ] NetworkPolicy generation (or an explicit, loud, documented deferral) scoping the authenticator Service to gateway-namespace-only ingress

### Add After Validation (v1.x)

- [ ] Config-reload-vs-pod-roll optimization — once it's clear which `shibboleth2.xml` sections `shibd` hot-reloads
- [ ] Well-known `/__auth/*` (`logout`/`whoami`) contract — promote from DESIGN §8 into a tracked requirement once the core reconcile loop is stable
- [ ] ReferenceGrant generation — needed the moment a non-Traefik attachment (GEP-1494/Envoy) is targeted
- [ ] Opt-in hostname-claim VAP + operator-side check — single-team cluster doesn't need it day one, but the VAP is already drafted (DESIGN §9)
- [ ] Deeper metrics/dashboards (Operator Capability Model L4 territory: alerting, log-based insights)

### Future Consideration (v2+)

- [ ] Cross-host centralized ACS / parent-domain cookie (already Out of Scope, phase 2)
- [ ] GEP-1494 `ExternalAuth`/Cilium-Envoy dataplane support (already Out of Scope, pending the 302-relay spike)
- [ ] Hub/Agents engine swap (already Out of Scope, pending protocol stabilization)
- [ ] App-queryable session API + force-logout/audit index (differentiators — real value, but riding on session-schema work that isn't core-path yet)
- [ ] OLM/OperatorHub packaging (only if a marketplace listing becomes a real go-to-market lever)
- [ ] Auto-pilot capability (Operator Capability Model L5: auto-scaling/auto-healing/auto-tuning) — not meaningful until there's fleet-scale operational data to tune against

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Status conditions + `observedGeneration` | HIGH | LOW | P1 |
| Finalizers (cross-ns cleanup) | HIGH | MEDIUM | P1 |
| Leader election | MEDIUM | LOW | P1 |
| Least-privilege RBAC (tracked) | MEDIUM | LOW | P1 |
| RequestMap collision handling | HIGH | HIGH | P1 |
| Edge header hygiene / clear-list | HIGH | MEDIUM | P1 |
| Real SP readiness probe | HIGH | MEDIUM | P1 |
| Config-hash-gated rollout | HIGH | MEDIUM | P1 |
| Memcached Sessions rendering (explicit) | HIGH | MEDIUM | P1 |
| NetworkPolicy generation (Service isolation) | HIGH | MEDIUM | P1 |
| CRD CEL validation | MEDIUM | LOW–MEDIUM | P1 |
| Prometheus metrics | MEDIUM | MEDIUM | P1 |
| Federation metadata refresh strategy (egress-restricted) | HIGH | MEDIUM–HIGH | P1 |
| Kubernetes Events on transitions | MEDIUM | LOW | P2 |
| ReferenceGrant generation | LOW (today) / HIGH (once non-Traefik) | LOW–MEDIUM | P2 |
| Well-known `/__auth/*` contract | HIGH | LOW–MEDIUM | P2 |
| Config-reload-vs-roll optimization | MEDIUM | HIGH | P2 |
| Hostname-claim VAP | LOW (single-team) | MEDIUM | P2 |
| App-queryable session API | MEDIUM | MEDIUM–HIGH | P3 |
| Force-logout/audit index | MEDIUM | LOW (given schema) | P3 |
| GEP-1494/Envoy attachment | HIGH (long-term) | HIGH | P3 |
| OLM/OperatorHub packaging | LOW (no current ask) | HIGH | P3 |

**Priority key:**
- P1: Must have for launch (production-grade claim depends on it)
- P2: Should have, add once core reconcile loop is proven stable
- P3: Nice to have, future consideration

## Competitor / Reference-Implementation Analysis

There isn't a direct "SAML-SP operator" competitor product to benchmark against (this space is dominated by OIDC-first tools); the useful comparison is against the *class* of production-grade K8s operators and against adjacent auth-gateway patterns.

| Feature | Generic production operator (OperatorHub L1-L2 norm) | oauth2-proxy / Keycloak-gatekeeper (OIDC-only forward-auth) | Our Approach |
|---------|---|---|---|
| Status reporting | Conditions + `observedGeneration` is the de facto standard for anything OLM-listed | N/A (not operator-managed, deployed as plain Deployment/sidecar) | Match the operator standard — already the strongest part of PROJECT.md Active |
| Session store | Varies; many just embed | Typically Redis for shared session/cookie storage | Memcached, chosen for SAML/Shibboleth-native fit and existing prior-production precedent (DESIGN §1, §8) |
| Auth protocol | N/A | OIDC/OAuth2 only — cannot do native SAML | Native SAML via Shibboleth SP v3 — this *is* the differentiator, since InCommon/higher-ed federations need real SAML, not an OIDC broker in front of it |
| Gateway attachment | N/A | Usually a sidecar/ingress-annotation model, tightly coupled to one ingress controller | Explicitly swappable attachment layer (Middleware today, GEP-1494 later) — directly answers the ingress-nginx-EOL lesson this project already lived through |
| Header hygiene at the edge | N/A | Typically documented as "trust your ingress to strip headers," rarely operator-enforced | Operator-generated clear-list per attachment model — a genuine differentiator versus the "hope the platform did it" norm |

## Sources

- [Kubernetes operator best practices: Implementing observedGeneration (Medium)](https://alenkacz.medium.com/kubernetes-operator-best-practices-implementing-observedgeneration-250728868792)
- [Good Practices — The Kubebuilder Book](https://book.kubebuilder.io/reference/good-practices)
- [Operator Capability Levels — Operator SDK](https://sdk.operatorframework.io/docs/overview/operator-capabilities/)
- [Operator capability levels — operatorframework.io](https://operatorframework.io/operator-capabilities/)
- [Stop Using Webhooks for CRD Validation | CEL Makes Kubernetes Admission Faster (Medium)](https://medium.com/@rameshavutu/bulletproof-validation-with-cel-ditch-your-admission-webhooks-96ee347232b1)
- [Admission Webhook Good Practices — Kubernetes docs](https://kubernetes.io/docs/concepts/cluster-administration/admission-webhooks-good-practices/)
- [Operator Best Practices — Operator SDK](https://sdk.operatorframework.io/docs/best-practices/best-practices/)
- [RBAC — The Kubebuilder Book](https://book.kubebuilder.io/reference/markers/rbac.html)
- [Using RBAC Markers in Kubernetes Operator Dev (oneuptime.com)](https://oneuptime.com/blog/post/2026-02-09-operator-rbac-markers/view)
- [Operator Observability Best Practices — Operator SDK](https://sdk.operatorframework.io/docs/best-practices/observability-best-practices/)
- [Metrics — The Kubebuilder Book](https://book.kubebuilder.io/reference/metrics.html)
- [FileBackedHTTPMetadataProvider — Shibboleth IdP4 wiki](https://shibboleth.atlassian.net/wiki/spaces/IDP4/pages/1265631639/FileBackedHTTPMetadataProvider)
- [XML Metadata Provider — Shibboleth SP3 wiki](https://shibboleth.atlassian.net/wiki/spaces/SP3/pages/2063696005/XMLMetadataProvider)
- [GEP-1494: HTTP Auth in Gateway API](https://gateway-api.sigs.k8s.io/geps/gep-1494/)
- [External Authorization — Envoy Gateway docs](https://gateway.envoyproxy.io/docs/tasks/security/ext-auth/)
- [Adding Forward Auth to a Gateway API HTTPRoute for Traefik](https://blog.inexplicity.de/adding-forward-auth-to-a-gateway-api-httproute-for-traefik.html)
- Internal: `DESIGN.md` §2, §5, §7, §8, §9, §9 addendum, §11 (spike-proven findings and open gaps)
- Internal: `.planning/PROJECT.md` Active/Out-of-Scope/Constraints
- Internal: `.planning/threads/saml-sp-operator.md` (spike fixes A–O)

---
*Feature research for: Kubernetes operator / SAML SP forward-auth authenticator*
*Researched: 2026-07-10*
