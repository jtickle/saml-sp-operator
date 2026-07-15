# Architecture Research

**Domain:** Two-controller, cross-namespace Kubernetes operator (controller-runtime / kubebuilder)
**Researched:** 2026-07-09
**Confidence:** HIGH (controller-runtime API surface, verified against v0.24.1 docs) / MEDIUM (the specific finalizer + single-writer resolutions below are this research's architectural recommendation, resolving ambiguity DESIGN.md left open — not scraped from an external source)

This file is scoped to **implementation architecture**, not domain choice — DESIGN.md §5–§7 already decided the CRD split, the finalizer-not-ownerRef call, and the aggregation model. This research makes that concrete: exact controller-runtime APIs, exact watch/index wiring, exact finalizer flow, and a dependency-ordered build sequence.

The project's own scaffold (branch `gsd/operator-scaffold`) pins **controller-runtime v0.24.1**, **k8s.io/apimachinery v0.36.0** — all API signatures below were checked against that exact version, not assumed from memory.

## Standard Architecture

### System Overview

```
┌─────────────────────────────── auth namespace ───────────────────────────────┐
│                                                                                │
│   SPInstance (CR) ──owns──▶ Deployment (shib-authenticator pod)               │
│        │                     ├─ ConfigMap (shibboleth2.xml + nginx.conf)      │
│        │                     ├─ Service (ClusterIP, admin/metrics)            │
│        │                     └─ Service (headless — ForwardAuth target)       │
│        │                                                                      │
│        │  ▲ Watches(AppIntegration) [index: .spec.spInstanceRef]             │
│        │  ▲ Watches(HTTPRoute)      [two-hop via targetRef index]            │
│        │  ▲ Watches(Secret)         [index: .spec.credentials.name]          │
│        │                                                                      │
│   SPInstanceReconciler:                                                      │
│     List bound+consented AppIntegrations → re-derive host/path from live     │
│     HTTPRoutes → shared collision algorithm → RequestMap → render →          │
│     hash → ConfigMap + pod-template annotation → rollout                     │
│                                                                                │
└────────────────────────────────────────────────────────────────────────────┬─┘
                                                                              │
                              cross-namespace boundary (finalizer, not ownerRef)
                                                                              │
┌─────────────────────────────── app namespace(s) ─────────────────────────┴──┐
│                                                                               │
│   AppIntegration (CR) ──owns──▶ Middleware (Traefik ForwardAuth,             │
│        │                          same-namespace ExtensionRef target)       │
│        │                                                                     │
│        │  ▲ Watches(HTTPRoute)   [index: .spec.targetRef.name, same-ns]     │
│        │  ▲ Watches(SPInstance)  [cross-ns Get for consent + Get status]    │
│        │                                                                     │
│   AppIntegrationReconciler:                                                  │
│     Resolve targetRef→HTTPRoute → resolve+validate SPInstance consent →     │
│     independently run the SAME shared collision algorithm over its own      │
│     sibling list → set Conflict/Degraded/Ready → render Middleware          │
│     (authResponseHeaders + per-model clear-list)                            │
│                                                                               │
│   HTTPRoute (Gateway API, user/GitOps-owned) ──ExtensionRef filter──▶ Middleware │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────┬─┘
                                                                              │
                                                          parentRef (cross-ns) │
                                                                              ▼
                                                    platform-owned Gateway (Traefik)
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|-------------------------|
| `SPInstanceReconciler` | Owns the SP Deployment/ConfigMap/Service/headless-Service; single writer of rendered `shibboleth2.xml`+`nginx.conf`; single writer of the aggregate RequestMap; drives config-hash-gated rollout | `ctrl.NewControllerManagedBy(mgr).For(&SPInstance{}).Owns(...).Watches(...)` |
| `AppIntegrationReconciler` | Owns its own Middleware; resolves its HTTPRoute; validates SPInstance consent; independently computes its own collision outcome; single writer of its own status conditions | Same builder pattern, namespace-scoped controller instance |
| Shared render/aggregation package (`internal/requestmap`, `internal/render`) | Pure, k8s-independent Go: RequestMap host/path partition + deterministic collision ordering + shibboleth2.xml/nginx.conf templating + config hash | Plain package, imported by **both** reconcilers — never a k8s object itself |
| Field indexers | Make cross-namespace/cross-kind fan-out queries O(1) instead of O(n) List+filter | `mgr.GetFieldIndexer().IndexField(ctx, obj, field, extractFunc)`, registered once in `main.go` before either controller's `SetupWithManager` |
| Finalizer (`saml.tickletechnologies.com/appintegration-cleanup`) | Guarantees a synchronous window between "AppIntegration deletion requested" and "AppIntegration actually gone," so the auth-namespace re-render is deterministic instead of racing GC | `controllerutil.AddFinalizer` / `ContainsFinalizer` / `RemoveFinalizer`, checked against `obj.GetDeletionTimestamp()` |

## Recommended Project Structure

```
api/v1alpha1/                  # already scaffolded — SPInstance, AppIntegration types
internal/
├── controller/
│   ├── spinstance_controller.go       # SPInstanceReconciler + SetupWithManager
│   ├── appintegration_controller.go   # AppIntegrationReconciler + SetupWithManager
│   └── index.go                       # field indexer registration (shared, called from main.go)
├── requestmap/                        # PURE, no k8s client — unit-testable without envtest
│   ├── collision.go                   # deterministic winner algorithm (oldest createdAt, UID tiebreak)
│   ├── partition.go                   # exact-Host vs HostRegex split, most-specific-path-first nesting
│   └── requestmap_test.go             # table-driven, fixtures = spike's proven shibboleth2.xml shapes
├── render/
│   ├── shibboleth2.go                 # shibboleth2.xml template (ApplicationDefaults/Sessions/MetadataProvider)
│   ├── nginxconf.go                   # nginx.conf template (X-Forwarded-* → fastcgi_param mapping)
│   ├── attributemap.go                # attribute-map.xml from AppIntegration.spec.attributes
│   ├── confighash.go                  # deterministic hash over rendered bytes
│   └── middleware.go                  # Traefik Middleware authResponseHeaders + clear-list per model
└── consent/
    └── consent.go                     # allowedNamespaces LabelSelector match — imported by SPInstance
                                        # controller ONLY (see "Consent is re-validated, not trusted" below)
```

### Structure Rationale

- **`internal/requestmap/` and `internal/render/` are plain Go with zero `client.Client` dependency.** This is the highest-leverage structural decision available: the trickiest logic in the whole system (collision ordering, host/path partitioning, XML templating) is exactly what the spike already proved by hand — pull it into pure functions, feed it the spike's known-good fixtures as golden tests, and it's fully unit-tested *before* any controller-runtime wiring exists. Both reconcilers import the same package, so they can never disagree about who wins a collision.
- **`internal/consent/` is imported only by the SPInstance controller.** This is deliberate: `allowedNamespaces` is a security boundary (which tenant may bind to which federation trust), so it must be re-evaluated by the trusted, central side, never taken on faith from a tenant-namespace object's self-reported status (see Anti-Pattern below).
- **`internal/controller/index.go` centralizes all `IndexField` registrations** in one place, called once from `main.go` before `SetupWithManager` on either controller — both controllers' `Watches()` calls depend on these indexes existing, so registration order matters and should not be scattered.

## Architectural Patterns

### Pattern 1: Field-indexed fan-out via `EnqueueRequestsFromMapFunc`

**What:** Register a field index so "give me all AppIntegrations pointing at SPInstance X" or "...pointing at HTTPRoute Y" is a cache lookup, not a full List+filter. Then wire a `Watches()` call whose map function does that lookup and returns the request(s) for the *other* controller's object.

**When to use:** Any time controller A's reconcile output depends on objects controller B manages elsewhere (or in another namespace), and B's changes must re-trigger A.

**Trade-offs:** Extra index registration boilerplate and one more thing to keep in sync at startup; but it's the only mechanism that scales — without it, either controller falls back to `List()` scanning the whole cluster on every reconcile of the other kind.

**Example** (SPInstance's fan-out from AppIntegration; verified against controller-runtime v0.24.1's actual `FieldIndexer`/`Builder` signatures):

```go
// internal/controller/index.go
const spInstanceRefIndex = "spec.spInstanceRef"

func SetupIndexes(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &samlv1alpha1.AppIntegration{},
		spInstanceRefIndex, func(obj client.Object) []string {
			ai := obj.(*samlv1alpha1.AppIntegration)
			ref := ai.Spec.SPInstanceRef
			return []string{ref.Namespace + "/" + ref.Name} // composite key: cross-ns ref
		}); err != nil {
		return err
	}
	return mgr.GetFieldIndexer().IndexField(ctx, &samlv1alpha1.AppIntegration{},
		"spec.targetRef.name", func(obj client.Object) []string {
			return []string{obj.(*samlv1alpha1.AppIntegration).Spec.TargetRef.Name}
		})
}

// spinstance_controller.go
func (r *SPInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&samlv1alpha1.SPInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Watches(&samlv1alpha1.AppIntegration{},
			handler.EnqueueRequestsFromMapFunc(r.mapAppIntegrationToSPInstance)).
		Watches(&gatewayv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.mapHTTPRouteToSPInstances)).
		Complete(r)
}

func (r *SPInstanceReconciler) mapAppIntegrationToSPInstance(
	ctx context.Context, obj client.Object) []reconcile.Request {
	ai := obj.(*samlv1alpha1.AppIntegration)
	return []reconcile.Request{{NamespacedName: types.NamespacedName{
		Name: ai.Spec.SPInstanceRef.Name, Namespace: ai.Spec.SPInstanceRef.Namespace,
	}}}
}

// Two-hop: HTTPRoute change → find AppIntegrations targeting it (same-ns index) →
// read each one's spInstanceRef → enqueue that SPInstance.
func (r *SPInstanceReconciler) mapHTTPRouteToSPInstances(
	ctx context.Context, obj client.Object) []reconcile.Request {
	route := obj.(*gatewayv1.HTTPRoute)
	var list samlv1alpha1.AppIntegrationList
	if err := r.List(ctx, &list, client.InNamespace(route.Namespace),
		client.MatchingFields{"spec.targetRef.name": route.Name}); err != nil {
		return nil
	}
	seen := map[types.NamespacedName]bool{}
	var reqs []reconcile.Request
	for _, ai := range list.Items {
		key := types.NamespacedName{Name: ai.Spec.SPInstanceRef.Name, Namespace: ai.Spec.SPInstanceRef.Namespace}
		if !seen[key] {
			seen[key] = true
			reqs = append(reqs, reconcile.Request{NamespacedName: key})
		}
	}
	return reqs
}
```

`AppIntegration`'s own `Watches(&samlv1alpha1.SPInstance{}, ...)` is the mirror image (no index needed there — the map is 1:1 direct from `Spec.SPInstanceRef`, a `Get`, not a `List`). Its `Watches(&HTTPRoute{})` is same-namespace and 1:1 via `spec.targetRef.name`.

### Pattern 2: Finalizer as a synchronous cross-namespace deletion gate

**What:** Because `ownerReferences` cannot cross namespaces (`metadata.ownerReferences` is validated against the object's own namespace — a namespaced object literally cannot reference an owner in a different namespace, and Kubernetes' GC controller will not cascade across that boundary even if you fake the UID), the only way to guarantee "something in another namespace has definitely observed my deletion before I actually vanish" is a finalizer on the object being deleted.

**When to use:** Exactly the AppIntegration→auth-namespace direction here. Without a finalizer: `kubectl delete appintegration foo` removes the object immediately; the SPInstance controller's watch *will* still fire (a Delete event triggers the same map function), but there is no guarantee the resulting reconcile completes, retries on transient failure, or beats a client that's already moved on (e.g. a CI pipeline deleting the AppIntegration and its namespace in the same script, or an app team assuming "deleted means fully retracted"). The finalizer converts "probably fine, eventually" into "the API server will not let this object disappear until the retraction is confirmed."

**Concrete flow for this project:**

```go
const appIntegrationFinalizer = "saml.tickletechnologies.com/appintegration-cleanup"

func (r *AppIntegrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ai samlv1alpha1.AppIntegration
	if err := r.Get(ctx, req.NamespacedName, &ai); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if ai.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(&ai, appIntegrationFinalizer) {
			// Cleanup: this AppIntegration is going away. The SPInstance
			// controller's own Watches(AppIntegration) already re-derives its
			// RequestMap by List()-ing live (non-deleting) AppIntegrations, so
			// there is no separate write this controller must perform here for
			// RequestMap correctness — List() naturally excludes an object mid-
			// deletion once the SPInstance reconciler filters on
			// DeletionTimestamp == nil (REQUIRED filter, see below).
			//
			// What genuinely needs a synchronous guarantee: if/when a
			// backendRef-based attachment model (GEP-1494/Envoy) is added, THIS
			// namespace's entry in the auth-namespace ReferenceGrant's `from`
			// list must be pruned once this is the last AppIntegration from this
			// namespace. Do that as a bounded, requeue-until-confirmed step here
			// (or trigger + poll the SPInstance's observedGeneration) before
			// removing the finalizer. Traefik ForwardAuth-by-URL needs no
			// ReferenceGrant at all, so v1 (Traefik-only) can safely finalize
			// immediately after this reconcile fires — see "Build order" below.
			controllerutil.RemoveFinalizer(&ai, appIntegrationFinalizer)
			return ctrl.Result{}, r.Update(ctx, &ai)
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&ai, appIntegrationFinalizer) {
		controllerutil.AddFinalizer(&ai, appIntegrationFinalizer)
		if err := r.Update(ctx, &ai); err != nil {
			return ctrl.Result{}, err
		}
	}
	// ... normal resolve/validate/render logic ...
}
```

**Trade-offs:** A finalizer that's never removed (bug in cleanup logic, or the controller is down) permanently blocks deletion — the classic footgun. Always: (1) make the cleanup path idempotent and retry-safe: (2) never block finalizer removal on a condition that can never become true if the cluster is partially torn down (e.g. don't wait on the SPInstance object itself, which might already be gone).

### Pattern 3: Shared pure aggregation, not cross-controller status writes

**What:** `Conflict` needs a global view (all AppIntegrations bound to one SPInstance), but `AppIntegration.status.Conditions` must be written **only** by the `AppIntegrationReconciler` — never by `SPInstanceReconciler` (see Anti-Pattern 1). Resolve this by giving both controllers the *same* pure collision function (`internal/requestmap`), fed by the *same* field index, so each independently derives the identical answer and writes only to the object it owns.

**When to use:** Any time two controllers need to agree on a derived fact without either writing into the other's status.

**Trade-offs:** Slight duplication of the List+compute step (SPInstance does it to build the RequestMap; AppIntegration does it again to know its own win/loss) — cheap because both operations are indexed lookups over a typically-small sibling set, and it completely avoids a write-triggers-a-write reconcile loop between the two controllers.

## Data Flow

### Config Render → Rollout Flow

```
AppIntegration created/updated
    ↓ (Watches, same-ns index on targetRef.name)
resolves HTTPRoute → hostnames/paths
    ↓ (Get, cross-ns, validated against allowedNamespaces — re-checked centrally)
validates SPInstance consent
    ↓ (writes its OWN status: SPInstanceResolved/RouteResolved/Conflict/Degraded/Ready)
    ↓ (this same create/update ALSO fires SPInstance's Watches(AppIntegration))
SPInstance reconcile:
    List all AppIntegrations where spInstanceRef == me AND DeletionTimestamp == nil
    ↓
re-validate allowedNamespaces per-candidate (do not trust their self-reported status)
    ↓
for each surviving candidate: re-Get its HTTPRoute, re-derive host/path (do not trust
resolvedHostnames status — it can be one reconcile stale)
    ↓
internal/requestmap: partition exact-Host vs HostRegex, apply deterministic collision
(oldest createdAt, UID tiebreak), nest overlapping paths most-specific-first
    ↓
internal/render: shibboleth2.xml + nginx.conf + attribute-map.xml
    ↓
hash rendered bytes (confighash.go)
    ↓
ConfigMap.Data = rendered content;  Deployment.Spec.Template.Annotations["saml.tickletechnologies.com/config-hash"] = hash
    ↓ (unchanged hash ⇒ ConfigMap patched but Deployment pod-template untouched ⇒ NO roll)
    ↓ (changed hash ⇒ pod-template annotation changes ⇒ Deployment controller rolls new ReplicaSet)
new pod starts → readiness probe MUST exercise a real shibd-backed handler (not bare
nginx 200) → only then does the rollout proceed to the next pod
```

**Why the hash lives on the pod template annotation, not just the ConfigMap:** a `ConfigMap` volume mount updates its files on disk in place (via kubelet sync) without restarting the pod — but `shibd` does not watch its config file for changes (DESIGN §11, open gap), so a silent file update would leave the running process on stale config. Stamping the hash onto `spec.template.metadata.annotations` is the standard "config checksum" trick (same mechanism Helm's `checksum/config` popularized) — it forces a genuine `Deployment` rollout only when the rendered bytes actually changed, which is exactly the config-hash-gated-rollout requirement.

### Key Data Flows

1. **Bind (AppIntegration created):** app team adds one CRD + one `ExtensionRef` line on their own HTTPRoute → AppIntegration resolves + validates → SPInstance re-renders + rolls (if hash changed) → AppIntegration's Middleware is created/updated in parallel, gated on the same collision computation.
2. **Unbind (AppIntegration deleted):** finalizer holds the object present for exactly one more SPInstance reconcile pass (which excludes it from the render because it's mid-deletion) → SPInstance rolls without it (if hash changed) → finalizer removed → object gone. The freed hostname/path is now available to the next-oldest surviving conflict-loser on its NEXT reconcile (naturally re-triggered because that loser is also matched by the same field index and gets requeued whenever a sibling under the same SPInstance changes — the map function should enqueue **all** siblings under one SPInstance whenever the SPInstance itself is reconciled and a status changes, OR simpler: have the SPInstance controller, after aggregation, explicitly patch each **affected** AppIntegration's Conflict status directly... **no** — that reintroduces the cross-controller-write anti-pattern. Correct fix: the collision-loser AppIntegration must itself be watched by whatever changed the set of siblings, i.e., it gets requeued because deleting a sibling under the same `spInstanceRef` is itself an event on the *AppIntegration* kind, and `AppIntegrationReconciler`'s own `Watches` should include **its siblings** (same index, same kind) precisely so a sibling's deletion re-triggers its own collision recomputation.
3. **Credential rotation (Secret updated):** `SPInstanceReconciler` watches the Secret named in `spec.credentials.name` (same-namespace index) → re-render → hash changes (new cert material is part of the rendered config surface, even if not textually in `shibboleth2.xml`, the Secret's resourceVersion or content hash should be folded into the config hash so a cert rotation forces a roll) → rollout.

## Consent Is Re-Validated Centrally, Not Trusted From Status

**This is a security-relevant architectural point, not a style preference.** `allowedNamespaces` on `SPInstance` is the boundary preventing an app team from binding to another tenant's federation trust by name (DESIGN §5). If `SPInstanceReconciler`'s aggregation step trusted `AppIntegration.status.SPInstanceResolved == True` (set by the `AppIntegrationReconciler`, which runs with the RBAC of the *operator*, but conceptually represents a claim made *about* a tenant-namespace object), a bug or a future privilege-adjacent change in that reconcile path could let a stale/incorrect status flag a non-consented app into the render. The central controller (`SPInstance`, auth-namespace, holds the private key) must independently evaluate the `LabelSelector` against the candidate's namespace on every aggregation pass — cheap (one `Get` on the `Namespace` object + `metav1.LabelSelectorAsSelector` match), and it makes the trust boundary self-contained in the one place that actually needs to enforce it.

## Anti-Patterns

### Anti-Pattern 1: Two controllers writing the same object

**What people do:** Have `AppIntegrationReconciler` directly patch the auth-namespace `ReferenceGrant` (or `Middleware`, or `SPInstance.status`) because it "has the information," while `SPInstanceReconciler` also writes to the same object as part of its own aggregation render.

**Why it's wrong:** Two independent reconcile loops racing to `Update`/`Patch` the same object produces resourceVersion-conflict thrashing at best, and a silently-incorrect "last writer wins" render at worst (exactly the failure mode DESIGN §7 explicitly rejects for RequestMap collisions — "never last-write-wins" should apply to every owned object, not just the RequestMap). controller-runtime's own `Owns()` model assumes one controller is the reconciling owner of a given kind.

**Do this instead:** Pick exactly one controller as the writer for every object kind (`SPInstance` → Deployment/ConfigMap/Service/headless-Service/[ReferenceGrant when it exists]; `AppIntegration` → its own Middleware and its own status only). Where a decision genuinely needs both sides' knowledge (Conflict), share a pure computation (Pattern 3), don't share write access.

### Anti-Pattern 2: Owning cross-namespace objects with `ownerReferences`

**What people do:** Set `AppIntegration` as an `ownerReference` on something living in the auth namespace (or vice versa), expecting Kubernetes garbage collection to clean it up.

**Why it's wrong:** `ownerReferences` are validated/enforced per-namespace; the API server permits setting a cross-namespace owner UID (it doesn't hard-reject it at admission in every version), but the GC controller's namespace-scoped cache lookups mean the cascade **silently never fires** — the object becomes permanently orphaned garbage with no error, no event, nothing in `kubectl describe` pointing at the problem. This is the exact trap DESIGN §7 already names.

**Do this instead:** Finalizers for the cross-namespace deletion-ordering guarantee (Pattern 2); same-namespace `Owns()`/`ownerReferences` for everything else.

### Anti-Pattern 3: Trusting resolved/derived status fields as reconcile inputs across a controller boundary

**What people do:** `SPInstanceReconciler` reads `AppIntegration.status.resolvedHostnames` instead of re-`Get`-ing the live `HTTPRoute` and re-deriving host/path itself.

**Why it's wrong:** Status can be one reconcile behind reality (the `AppIntegration` controller might not have processed a recent `HTTPRoute` edit yet), and RequestMap correctness is a security property (fix N from the spike: a RequestMap miss means the authorizer **fails open**). A stale derived value silently under- or over-protects a route.

**Do this instead:** Treat another controller's `status` as a *hint for human/operator visibility* only. Re-derive anything that feeds into a security-relevant render (RequestMap, consent) directly from the primary source object (`HTTPRoute`, `Namespace` labels) on every aggregation pass.

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|---------------------------|
| Single SPInstance, handful of AppIntegrations | Exactly the design above; List()-based aggregation on every reconcile is trivially cheap. |
| One SPInstance, dozens–low-hundreds of bound AppIntegrations | Field indexes (already required) keep List() calls indexed rather than full-table scans; consider a short requeue debounce (`RateLimiter`/a small `after:` delay) on the SPInstance reconcile so a burst of sibling AppIntegration edits collapses into one render+roll instead of N. |
| Many SPInstances (multi-tenant auth namespaces, or per-team SPInstance) | Each SPInstance's aggregation is already namespace/`spInstanceRef`-scoped, so this scales horizontally without design changes — the operator's own manager can shard by leader election as usual; no new coordination needed between SPInstances (they don't share state). |

### Scaling Priorities

1. **First bottleneck: unnecessary rollouts from unrelated reconciles.** The config-hash gate already exists for this reason — validate it early (Phase B below) so every later phase inherits a working "don't roll when nothing changed" guarantee.
2. **Second bottleneck: reconcile storms from tight watch loops.** Two controllers watching each other's Owns()/Watches() graph can create feedback loops if either writes something the other watches without an actual content change (e.g., `Update`-ing status every reconcile even when nothing changed). Use `equality.Semantic.DeepEqual`-guarded status writes and `predicate.GenerationChangedPredicate`/`predicate.ResourceVersionChangedPredicate` where appropriate to suppress no-op-triggered reconciles.

## Suggested Build Order (dependency-ordered)

Given the scaffold already exists (empty reconcilers, CRD types, envtest green) and the spike already proved the entire config surface end-to-end, the build order should front-load the riskiest *new* logic (collision ordering, templating) as pure code, then build outward one controller at a time, each phase independently verifiable against the live spike image before the next phase adds cross-namespace complexity.

**Phase A — Shared render/aggregation package (`internal/requestmap`, `internal/render`), no controller-runtime yet.**
Pure Go, unit-tested against fixtures lifted directly from the spike's proven `shibboleth2.xml`/`nginx.conf`/`attribute-map.xml`. Covers: host/path partition (exact vs regex), deterministic collision ordering (oldest `createdAt`, UID tiebreak), nested-path construction, template rendering, config hashing. Zero k8s dependency — fastest to iterate, and de-risks the one piece of genuinely new logic (the spike never had to arbitrate between multiple apps).
*Why first:* nothing downstream can be correctly tested until this exists, and it needs no cluster to validate.

**Phase B — `SPInstanceReconciler`, static path (no AppIntegration awareness yet).**
`For(&SPInstance{})`, `Owns(Deployment/ConfigMap/Service/headless-Service)`. Render config from the `SPInstance` spec alone (entityID, IdP metadata, credentials Secret) with an empty RequestMap. Wire the config-hash-gated rollout and a real SP readiness probe (proves `shibd` loaded, not a bare 200 — carried over from spike fix J's caveat). Wire the Secret watch (credential rotation) here too, since it's same-namespace and simple.
*Why second:* this alone reproduces "bring up a bare authenticator" against the live cluster, validating the render→ConfigMap→hash→rollout mechanics — the highest-risk *mechanical* piece — before any cross-namespace fan-out exists to complicate debugging.

**Phase C — `AppIntegrationReconciler`, resolution only (no Middleware yet).**
`For(&AppIntegration{})`, `Watches(HTTPRoute)` same-namespace via the `targetRef.name` index, `Watches(SPInstance)` cross-namespace (1:1 `Get`, no index needed on this side). Resolve the route, `Get` the `SPInstance` and validate consent (still authoritative-side-checked in Phase D; this phase can do a first-pass local check too, but must not be the final word), set `SPInstanceResolved`/`RouteResolved`/`Degraded` conditions and `resolvedHostnames` status. No `Conflict`, no Middleware yet.
*Why third:* independently verifiable (a status-only controller is easy to test in isolation) and doesn't yet touch the render pipeline from Phase A/B.

**Phase D — Cross-namespace aggregation on the `SPInstance` side.**
Add the field indexes (`spec.spInstanceRef` composite key, if not already registered in Phase C) and `Watches(AppIntegration)` + the two-hop `Watches(HTTPRoute)` map function on `SPInstanceReconciler`. Re-validate consent centrally (per "Consent Is Re-Validated Centrally" above) using live `Namespace` labels, not `AppIntegration.status`. Feed the real, multi-app RequestMap through Phase A's collision algorithm into the Phase B render pipeline. This is the point at which binding a second app can, for the first time, actually collide with the first.
*Why fourth:* depends on A (algorithm), B (render pipeline it plugs into), and C (the objects it's now aggregating existing and being resolvable).

**Phase E — `AppIntegration` Middleware emission, self-determined `Conflict`, and the finalizer.**
`Owns(Middleware)` same-namespace. Give `AppIntegrationReconciler` the same Phase A collision function over its own sibling `List()` (same index as Phase D) to set `Conflict` on itself without any cross-controller status write. Render `authResponseHeaders` from `.spec.attributes` plus the per-model header-hygiene clear-list (Traefik enumerate-clear, per DESIGN §11's spike-level decision). Add the `saml.tickletechnologies.com/appintegration-cleanup` finalizer (Pattern 2) — for Traefik-only v1 (no `ReferenceGrant` yet, since `ForwardAuth`-by-URL doesn't need one), the cleanup step can safely be "fire one more sibling requeue, then remove finalizer immediately" rather than the more elaborate confirm-then-remove handshake.
*Why fifth/last:* this is the true end-to-end integration point — "add one CRD, app becomes protected" — and it depends on every prior phase (the render pipeline, the live SPInstance, resolution, and aggregation all being correct first).

**Explicitly deferred past this build (flag for later phases/milestones, not blocking v1):**
- `ReferenceGrant` lifecycle management — only needed once a real `backendRef`-based attachment model (GEP-1494/Envoy `SecurityPolicy`) exists; Traefik `ForwardAuth`-by-URL sidesteps it entirely (DESIGN §5). When it lands, make `SPInstance` the sole writer (derived from the same aggregation pass as RequestMap), per Anti-Pattern 1 — do not let `AppIntegration`'s finalizer patch it directly.
- Config **reload vs. roll** investigation (DESIGN §11 open gap) — could let routine `AppIntegration` edits signal a reload instead of a full pod roll; not required for correctness, an optimization for later.
- The confirm-then-remove finalizer handshake (vs. the simpler fire-and-remove used in Phase E) — worth hardening once `ReferenceGrant` pruning becomes a real cross-namespace side effect that must not silently lag.

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|----------------------|-------|
| Gateway API `HTTPRoute` (user/GitOps-owned) | `Watches()`, read-only `Get`/`List` — the operator never writes to it (DESIGN §9: "operator co-editing the app's route is rejected: GitOps reverts it") | Both controllers read host/path/query only; header/method-based matches are undecidable from RequestMap and must surface `Degraded`. |
| Traefik `Middleware` (`traefik.io` CRD) | `Owns()` from `AppIntegrationReconciler`, same-namespace only (Traefik's `ExtensionRef` is a `LocalObjectReference` — cannot cross namespaces) | Swappable attachment layer per DESIGN §9 addendum; this is the CRD kind that changes if/when GEP-1494 `ExternalAuth`/Envoy `SecurityPolicy` is adopted — the two CRDs and the render pipeline stay unchanged. |
| Platform `Gateway` (Traefik, `traefik` namespace) | Cross-namespace `parentRef` on the app's own `HTTPRoute` (not operator-managed) | Operator never owns or reconciles the `Gateway` object itself. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|----------------|-------|
| `AppIntegrationReconciler` ↔ `SPInstanceReconciler` | Field-indexed `Watches()` fan-out in both directions (Pattern 1); **no direct status writes across the boundary** (Anti-Pattern 1) | The only "shared state" is the pure `internal/requestmap` computation both independently invoke (Pattern 3). |
| `AppIntegration` deletion ↔ `SPInstance` render | Finalizer (Pattern 2) | Guarantees the render excludes a deleting `AppIntegration` deterministically rather than racing GC. |
| Rendered config ↔ Deployment rollout | Config-hash stamped on `spec.template.metadata.annotations`, never on the top-level object metadata | The one mechanism that reliably forces a real rolling restart when `shibd` itself won't hot-reload (DESIGN §11 open gap). |

## Sources

- [controller-runtime `pkg/builder` — pkg.go.dev v0.24.1](https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/builder) — verified exact `Watches`/`Owns` signatures against the version this project's scaffold pins. HIGH confidence (official reference docs).
- [controller-runtime `pkg/client` `FieldIndexer`/`IndexerFunc` — pkg.go.dev v0.24.1](https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/client#FieldIndexer) — verified `IndexField` signature and the documented "namespaced objects get namespaced+non-namespaced index variants automatically" behavior, load-bearing for same-namespace lookups (Secret rotation, `targetRef.name`). HIGH confidence.
- [`kubernetes-sigs/controller-runtime` — cross-namespace watch discussion, Issue #687](https://github.com/kubernetes-sigs/controller-runtime/issues/687) and [Issue #1330 on `Watches`+`EnqueueRequestsFromMapFunc`](https://github.com/kubernetes-sigs/controller-runtime/issues/1330) — confirms the map-function fan-out pattern is the maintainer-sanctioned mechanism for this exact cross-namespace/cross-kind case. HIGH confidence.
- [`kubernetes-sigs/controller-runtime` `pkg/controller/controllerutil`](https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/controller/controllerutil/controllerutil.go) — `AddFinalizer`/`ContainsFinalizer`/`RemoveFinalizer` signatures used in Pattern 2. HIGH confidence.
- Project's own `DESIGN.md` §5–§7, §9, §11 and `.planning/PROJECT.md` — authoritative for the CRD shapes, the finalizer-not-ownerRef decision, the RequestMap collision rules, and the header-hygiene clear-list. Ground truth, not "confidence-scored" external research.
- `gsd/operator-scaffold` branch (`api/v1alpha1/*_types.go`, `cmd/main.go`, `internal/controller/*_controller.go`, `go.mod`) — read directly to confirm the exact field names (`SPInstanceRef`, `TargetRef.Name`, `Credentials.Name`, `AllowedNamespaces *metav1.LabelSelector`) and the pinned controller-runtime version this research targets.
- The single-writer / anti-pattern reasoning (Anti-Pattern 1–3, the consent re-validation call, and the finalizer-depth recommendation in Pattern 2/Phase E) is this research's own synthesis resolving ambiguity DESIGN.md left open — flagged MEDIUM confidence, presented as an opinionated recommendation for the roadmap to adopt or override, not a scraped external fact.

---
*Architecture research for: two-controller cross-namespace Kubernetes operator (SAML SP operator)*
*Researched: 2026-07-09*
