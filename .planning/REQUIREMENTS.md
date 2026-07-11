# Requirements: SAML SP Operator

**Defined:** 2026-07-09
**Core Value:** An app team adds one `AppIntegration` and their app is SAML-authenticated behind Gateway API — no hand-written `shibboleth2.xml`.

> "User" here = the platform/app team operating the cluster. Requirements are testable operator capabilities. Categories follow the architecture's build seams (research ARCHITECTURE.md): a pure render package, then the two controllers, then cross-cutting operability + security.

## v1 Requirements

### Render & Aggregation (pure `internal/render` package — no k8s deps, unit-tested against spike fixtures)

- [ ] **RENDER-01**: Render `shibboleth2.xml` (ApplicationDefaults, `Sessions`, `MetadataProvider`, credentials) from an `SPInstance` spec, via `encoding/xml` struct marshaling (not string templating)
- [x] **RENDER-02**: Derive the SP self-URL config — `SHIBSP_SERVER_{NAME,SCHEME,PORT}` env + absolute `handlerURL` — from each app's external URL (spike fix M)
- [ ] **RENDER-03**: Render `attribute-map.xml` from `AppIntegration.attributes` (SAML attribute id → exported `Variable-<id>` header)
- [ ] **RENDER-04**: Aggregate all bound `AppIntegration`s into one ordered RequestMap (host/path → applicationId); most-specific path first; exact `<Host>` before `<HostRegex>`
- [ ] **RENDER-05**: Every RequestMap `<Host>` carries explicit `scheme`+`port` (spike fix N — the gate fails **open** otherwise)
- [x] **RENDER-06**: RequestMap collision on `(hostname, path)` → deterministic winner by sort key `(priority desc, createdAt asc, UID asc)` — explicit `AppIntegration.spec.priority` (int32, higher wins, default 0) is consulted first, then oldest `createdAt`, then UID as the final tiebreak; loser excluded from render and flagged — never last-write-wins, and deterministic across map iterations
- [ ] **RENDER-07**: Render `nginx.conf` (X-Forwarded-* → FastCGI params, `/authcheck` + `/Shibboleth.sso` locations) via `text/template`
- [ ] **RENDER-08**: Generate the edge header-hygiene clear-list per attachment model (enumerate-clear for Traefik; `Variable-*` glob for the nginx model)
- [ ] **RENDER-09**: Compute a `sha256` config hash over the rendered bytes for rollout gating
- [ ] **RENDER-10**: Rendered config is injection-safe against hostile CRD string fields (XML-escaped; no `--` in comments — spike fix K)

### SPInstance Controller (auth namespace)

- [ ] **SPI-01**: Reconcile an `SPInstance` into a running SP Deployment + Service + **headless** Service + ConfigMap
- [ ] **SPI-02**: Roll the SP Deployment only when the config hash changes (pod-template annotation) — unrelated reconciles don't churn the fleet
- [ ] **SPI-03**: Readiness probe that proves `shibd` actually loaded (exercises a real handler), not a dumb nginx 200
- [ ] **SPI-04**: Aggregate all bound `AppIntegration`s (cross-namespace) into the rendered config via field index + watch, re-validating host/consent in the trusted controller (never trusting `AppIntegration.status`)
- [ ] **SPI-05**: Wire the memcached `Sessions`/`StorageService` when `sessionStore` is set
- [ ] **SPI-07**: Fail-safe rollout — the SP Deployment uses `RollingUpdate` with `maxUnavailable: 0`, so a config change whose new pod fails readiness (shibd won't load it) can never retire a healthy pod; the last-good config keeps serving (the ingress-nginx property). Pairs with SPI-02 (hash-gated *when* to roll) and SPI-03 (readiness proves shibd loaded). *(SPI-06 reserved for the v2 metadata mirror; kept distinct rather than renumbered.)*

### AppIntegration Controller (app namespace)

- [ ] **APP-01**: Resolve the target `HTTPRoute` (hostnames + path matches); surface `Degraded` on header/method-only matches (not RequestMap-derivable)
- [ ] **APP-02**: Validate `SPInstance` consent (`allowedNamespaces`) before binding
- [ ] **APP-03**: Emit the Traefik ForwardAuth `Middleware` targeting the **headless** Service, with `authResponseHeaders` = the attribute→header list (spike fix O + Hyp #3)
- [ ] **APP-04**: Compute its own `Conflict` independently from the shared render package
- [ ] **APP-05**: Cross-namespace lifecycle via finalizer — re-render the SP config on delete (ownerRefs can't span namespaces)

### Observability & Status

- [ ] **OBS-01**: `AppIntegration` status conditions (`SPInstanceResolved`/`RouteResolved`/`Conflict`/`Degraded`/`Ready`) + `observedGeneration`
- [ ] **OBS-02**: `SPInstance` status (`ConfigRendered`/rollout health/bound-count) + `observedGeneration`
- [ ] **OBS-03**: Surface the generated SP **metadata URL** in `SPInstance` status (hand to IdP admins for registration)
- [ ] **OBS-04**: Emit Kubernetes Events on significant reconcile transitions (rendered, rolled, conflict, degraded)
- [ ] **OBS-05**: Expose Prometheus metrics (controller-runtime defaults + reconcile/render/rollout counters)

### Security & Operability

- [ ] **SEC-01**: Generate a NetworkPolicy so the authenticator Service is reachable **only** from the gateway (enforces the `X-Forwarded-Host` trust boundary in code, not prose)
- [ ] **SEC-02**: Keep the SP private key isolated to the auth namespace (RBAC + informer cache scoping — not readable per-tenant)
- [ ] **SEC-03**: Reject invalid specs at admission via CRD validation (CEL / OpenAPI) — e.g. malformed external URL, missing credentials
- [ ] **SEC-04**: Basic SSRF guard on the IdP metadata-URL fetch (require https, reject link-local/private targets)
- [ ] **OPS-01**: Leader election enabled (single active reconciler across replicas)
- [ ] **OPS-02**: Least-privilege RBAC generated from `+kubebuilder:rbac` markers, scoped to what the controllers touch

## v2 Requirements

Deferred — tracked, not in the current roadmap.

### Federation & Portability

- **SPI-06**: In-cluster federation-metadata **mirror/refresh** for egress-restricted clusters (v1 supports a reachable remote metadata URL + documents the egress requirement)
- **ATTACH-06**: GEP-1494 `ExternalAuth` attachment for Cilium/Envoy dataplanes (needs the 302-relay spike first) + `ReferenceGrant` generation for `backendRef`-based attachment
- **SEC-05**: Signed federation metadata verification filter (`MetadataFilter` + signing cert) for real InCommon feeds

### Operations

- **OPS-03**: `shibd` hot-reload for routine RequestMap edits (skip a full pod roll) — pending the reload-vs-roll empirical classification (DESIGN §11)
- **OPS-04**: OLM / Helm packaging for distribution

## Out of Scope

| Feature | Reason |
|---------|--------|
| Writing our own SAML core | Orchestration is the value; XML-DSIG is a bypass-minefield (DESIGN §2) |
| Per-app entityID / `ApplicationOverride` by default | Federation anti-pattern; one entityID per deployment (DESIGN §5) |
| Building on Shibboleth Hub/Agents | Draft, needs a Java IdP runtime; future swappable engine (DESIGN §3) |
| Operator owning the Gateway | Attaches to a platform-provided Gateway; never owns it (DESIGN §9) |
| Cross-host centralized ACS | Phase-2 product chapter after single-host ships (DESIGN §11) |
| Mutating admission webhook that rewrites specs | Operator anti-pattern; validate + condition instead |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| RENDER-01 | Phase 1 | Pending |
| RENDER-02 | Phase 1 | Complete |
| RENDER-03 | Phase 1 | Pending |
| RENDER-04 | Phase 1 | Pending |
| RENDER-05 | Phase 1 | Pending |
| RENDER-06 | Phase 1 | Complete |
| RENDER-07 | Phase 1 | Pending |
| RENDER-08 | Phase 1 | Pending |
| RENDER-09 | Phase 1 | Pending |
| RENDER-10 | Phase 1 | Pending |
| SPI-01 | Phase 2 | Pending |
| SPI-02 | Phase 2 | Pending |
| SPI-03 | Phase 2 | Pending |
| SPI-05 | Phase 2 | Pending |
| SPI-07 | Phase 2 | Pending |
| OBS-03 | Phase 2 | Pending |
| OBS-05 | Phase 2 | Pending |
| SEC-01 | Phase 2 | Pending |
| SEC-02 | Phase 2 | Pending |
| SEC-03 | Phase 2 | Pending |
| OPS-01 | Phase 2 | Pending |
| APP-01 | Phase 3 | Pending |
| APP-02 | Phase 3 | Pending |
| SPI-04 | Phase 4 | Pending |
| OBS-02 | Phase 4 | Pending |
| APP-03 | Phase 5 | Pending |
| APP-04 | Phase 5 | Pending |
| APP-05 | Phase 5 | Pending |
| OBS-01 | Phase 5 | Pending |
| SEC-04 | Phase 6 | Pending |
| OPS-02 | Phase 6 | Pending |
| OBS-04 | Phase 6 | Pending |

**Coverage:**

- v1 requirements: 32 total (RENDER-01..10 + SPI-01..05,07 + APP-01..05 + OBS-01..05 + SEC-01..04 + OPS-01..02 = 32; SPI-07 fail-safe rollout added 2026-07-10 during Phase 1 discussion)
- Mapped to phases: 32/32 ✓
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-09*
*Last updated: 2026-07-10 during Phase 1 discussion — added SPI-07 (fail-safe rollout, `maxUnavailable: 0`) mapped to Phase 2; RENDER-06 sort key extended with `AppIntegration.spec.priority`; count 31 → 32*
</content>
