# SAML SP Operator

## What This Is

A Kubernetes operator (Go / kubebuilder) that wraps a containerized **Shibboleth
SP v3** as a gateway-portable, forward-auth authenticator. App teams drop an
`AppIntegration` next to their `HTTPRoute`; the operator renders the Shibboleth +
nginx config, runs the SP, and wires the gateway's forward-auth so the app is
protected by real SAML without the app team touching SAML at all. It **owns the
orchestration and borrows the SAML** — the operator does zero cryptography.

## Core Value

Reconcile two CRDs (`SPInstance`, `AppIntegration`) + Gateway API `HTTPRoute`s
into a **working forward-auth SAML SP** — an app team adds one CRD and their app
is authenticated, cross-gateway-portable, with no hand-written `shibboleth2.xml`.

## Business Context

<!-- Tickle Technologies product; revenue model still TBD (possibly open core + paid support/SaaS). -->

- **Customer**: teams running apps behind Kubernetes Gateway API that need native SAML (higher-ed / InCommon-style federations especially)
- **Revenue model**: TBD — likely open core + paid support/SaaS
- **Success metric**: an app goes from unprotected to SAML-authenticated by adding one `AppIntegration`, no SAML expertise required
- **Strategy notes**: GPL/Apache-leaning; public repo (`github.com/jtickle/saml-sp-operator`)

## Requirements

### Validated

<!-- The de-risking spike PROVED the config surface end-to-end, but nothing operator-generated has shipped. These are feasibility-proven, not shipped. -->

(None shipped yet — the spike proved feasibility; see Context. Ship the operator to validate.)

### Active

<!-- v1.0 operator. Hypotheses until shipped. -->

- [ ] Operator reconciles an `SPInstance` into a running SP Deployment + Service (+ headless Service) + ConfigMap
- [ ] Operator renders `shibboleth2.xml` (ApplicationDefaults, Sessions, MetadataProvider, credentials) from `SPInstance` spec
- [ ] Operator renders the SP self-URL config from each app's external URL (`SHIBSP_SERVER_{NAME,SCHEME,PORT}` env + absolute `handlerURL`)
- [ ] `AppIntegration` controller resolves its target `HTTPRoute` and validates `SPInstance` consent (`allowedNamespaces`)
- [ ] Operator aggregates all bound `AppIntegration`s into one ordered RequestMap (host/path → applicationId), emitting explicit `scheme`+`port` on each `<Host>`
- [ ] RequestMap collision handling: deterministic winner (oldest `createdAt`), loser gets a `Conflict` condition — never last-write-wins
- [ ] Operator emits the Traefik ForwardAuth `Middleware` targeting a **headless** Service, with `authResponseHeaders` = the app's attribute→header mapping
- [ ] Operator renders the attribute-map (SAML attribute ids → exported `Variable-<id>` headers) from `AppIntegration.attributes`
- [ ] Config-hash-gated rollout: the SP Deployment rolls only when the rendered config changes (unrelated reconciles don't churn the fleet)
- [ ] Real SP readiness probe that proves shibd loaded (not a dumb nginx 200)
- [ ] Status conditions surfaced: `SPInstanceResolved`/`RouteResolved`/`Conflict`/`Degraded`/`Ready` on AppIntegration; `ConfigRendered`/rollout health/bound-count/**SP metadata URL** on SPInstance
- [ ] Cross-namespace lifecycle via finalizers (not ownerRefs — they're namespace-local)
- [ ] Edge header hygiene: operator emits the per-model clear-list so client-injected identity headers can't reach the app

### Out of Scope

- **Writing our own SAML core** — the value is orchestration, not crypto; XML-DSIG is a bypass-minefield. Borrow a vetted engine. (DESIGN §2)
- **A distinct entityID / `ApplicationOverride` per app by default** — federation anti-pattern; one entityID per deployment, differentiate by per-path settings. (DESIGN §5)
- **Building on Shibboleth Hub/Agents now** — draft/brand-new, needs a Java IdP runtime; treat as a future swappable engine. (DESIGN §3)
- **The operator owning the Gateway** — it attaches routes + Middleware to a platform-provided Gateway; never owns the Gateway. (DESIGN §9)
- **Cross-host centralized ACS** (dedicated auth host + parent-domain cookie) — phase-2 follow-up after the single-host operator ships. (DESIGN §11)
- **GEP-1494 `ExternalAuth` / Cilium-Envoy dataplane support** — designed-for (swappable attachment layer) but a later chapter; the 302-relay is an unproven portability unknown pending its own spike. (DESIGN §9 addendum)

## Context

- **The de-risking spike is COMPLETE and PROVEN** (see `.planning/threads/saml-sp-operator.md` and `spike/`). A full browser round-trip works in-cluster: browser → mocksaml → session → Traefik ForwardAuth gate → app, with identity attributes flowing. All four DESIGN §10 hypotheses are green. The spike proved the *entire config surface* the operator must generate.
- **The API scaffold already exists** on branch `gsd/operator-scaffold`: kubebuilder go/v4 project, both CRDs under `saml.tickletechnologies.com/v1alpha1` with first-cut types, generated CRDs/RBAC, empty reconcilers, envtest green.
- **Authoritative design**: `DESIGN.md` §1–§12 is the decision record (problem, the own-orchestration/borrow-SAML framing, engine choice, encapsulation trick, CRD model, request mapping, operator design, sessions, Gateway attachment, spike, open gaps).
- **Repo is PUBLIC** — keep employer/infrastructure identifiers out of every commit, planning doc, and message.

## Constraints

- **Tech stack**: Go + kubebuilder / controller-runtime; the SP engine is containerized Shibboleth SP v3 (FastCGI `shibauthorizer`/`shibresponder` + `shibd` + stock nginx, one pod). The operator does zero SAML.
- **Gateway portability**: nothing gateway-implementation-specific may leak into the durable design (Traefik now, Cilium/Envoy possible later). The attachment layer is swappable; the SP image is unchanged across dataplanes.
- **Swappable-engine boundary**: the one known leak is exported-header *naming* (SP3 hard-codes the `Variable-` prefix) — swapping the engine is a header-contract change. (DESIGN §2, §9 addendum)
- **Sessions**: shared **memcached** for cross-replica sessions + SLO; rolling restart is safe only because sessions are external.
- **Egress-restricted clusters**: remote federation metadata (InCommon) can't be fetched without an egress allowance or an in-cluster metadata mirror.
- **Security**: the authenticator Service must be reachable only from the gateway (applicationId/authz depend on `X-Forwarded-Host`, otherwise client-spoofable); the gateway must strip client-supplied identity headers.
- **Namespaces**: `SPInstance` central in the auth ns (private key not readable per-tenant); `AppIntegration` same-namespace as its HTTPRoute (Gateway API policy attachment is namespace-local by design).

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Own the orchestration, borrow the SAML | Value is orchestration; XML-DSIG is a bypass-minefield to write ourselves | ✓ Good — validated by spike |
| Shibboleth SP v3 as the engine (not Hub/Agents) | Battle-tested, InCommon-grade; Hub/Agents is draft + needs a Java IdP runtime | ✓ Good |
| Encapsulate `nginx + shib FastCGI + shibd` in one pod, nginx as in-pod FastCGI→HTTP adapter | Makes the SP gateway-portable; Traefik ForwardAuth supplies the FastCGI-authorizer semantics | ✓ Good — proven in spike |
| Two CRDs (`SPInstance` auth-ns, `AppIntegration` app-ns) | Split along SAML truth (entity vs binding) + namespace security | ✓ Good |
| API group `saml.tickletechnologies.com` | Reverse-DNS on a company domain Jeff owns; decouples CRD identity from personal domain | ✓ Good |
| `SHIBSP_SERVER_{NAME,SCHEME,PORT}` process env is the authoritative self-URL knob | fastcgi_params do NOT drive scheme reconstruction (spike fix M) | ✓ Good — proven |
| RequestMap `<Host>` needs explicit `scheme`+`port` on non-standard ports | Otherwise the authorizer fails OPEN (spike fix N) — the single most important security lesson | ✓ Good — proven |
| ForwardAuth targets a headless Service, not the ClusterIP | ClusterIP is CNI/kube-proxy-fragile from the gateway ns (spike fix O) | ✓ Good — proven |
| Cross-namespace coordination via finalizers, not ownerRefs | ownerRefs are namespace-local; cross-ns GC silently won't fire | — Pending (build) |
| Edge header hygiene per attachment model (Traefik can't wildcard-strip; nginx can) | Unlisted `Variable-*` rides through under Traefik ForwardAuth (spike-verified) | ✓ Good — proven |
| `internal/render` stays a pure Go library — no k8s (apimachinery/CRD) imports; controllers adapt CRD specs into plain-Go render inputs | The render core is the shared seam between this operator (Traefik ForwardAuth attachment) and a planned standalone single-container deployment (nginx `auth_request` attachment). Keeping it k8s-free lets both consume it without a base-container merge that would be wrong. RENDER-08's per-attachment-model clear-list already encodes the two-consumer reality. The "no k8s dep" line in REQUIREMENTS.md is this, not arbitrary purity | — Pending (Phase 1) |
| Fail-safe rollout is a hard guarantee: a config push can never break a running SP (the ingress-nginx property) | Three independent nets — (1) build-time: valid render input can only produce shibd-loadable output, proven by the real-shibd load test in CI (our `nginx -t` analog); (2) admission-time: CEL rejects malformed specs (SEC-03); (3) runtime: readiness probe proves shibd actually loaded (SPI-03) + Deployment `RollingUpdate` with **`maxUnavailable: 0`**, so a bad pod never replaces a good one and the old config keeps serving. Losing any one net still leaves the SP serving | — Pending (Phase 1 load test + Phase 2 readiness/rollout) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Business Context check — customer, revenue model, success metric still accurate?
4. Audit Out of Scope — reasons still valid?
5. Update Context with current state

---
*Last updated: 2026-07-09 after initialization*
