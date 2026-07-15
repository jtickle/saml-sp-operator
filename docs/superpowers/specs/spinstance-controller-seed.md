# SPInstance Controller — slice seed

**Not a spec.** A starting note for the first Superpowers brainstorm of the
SPInstance controller slice, lifted from the archived GSD roadmap/requirements
(`docs/superpowers/archive/gsd-planning/`). The brainstorm produces the dated
design spec; this only gives it a running start.

## Slice goal

A single `SPInstance` CR becomes a real, running, production-hardened SP
Deployment in the auth namespace — config-hash-gated rollout, real readiness,
memcached sessions, least-privilege secret access, network-isolated,
leader-elected — before any `AppIntegration` exists.

## Requirements to cover

- **SPI-01** — Reconcile an `SPInstance` into a running SP Deployment + Service
  + **headless** Service + ConfigMap.
- **SPI-02** — Roll the SP Deployment only when the config hash changes
  (pod-template annotation); unrelated reconciles don't churn the fleet.
- **SPI-03** — Readiness probe that proves `shibd` actually loaded (exercises a
  real handler), not a dumb nginx 200.
- **SPI-05** — Wire the memcached `Sessions`/`StorageService` when
  `sessionStore` is set.
- **SPI-07** — Fail-safe rollout: `RollingUpdate` with `maxUnavailable: 0`, so a
  config change whose new pod fails readiness can never retire a healthy pod
  (the ingress-nginx property). Pairs with SPI-02 (when to roll) and SPI-03
  (readiness proves shibd loaded).
- **OBS-03** — Surface the generated SP **metadata URL** in `SPInstance` status.
- **OBS-05** — Expose Prometheus metrics (controller-runtime defaults +
  reconcile/render/rollout counters).
- **SEC-01** — Generate a NetworkPolicy so the authenticator Service is
  reachable **only** from the gateway.
- **SEC-02** — Keep the SP private key isolated to the auth namespace (RBAC +
  informer-cache scoping).
- **SEC-03** — Reject invalid specs at admission via CRD validation (CEL /
  OpenAPI): malformed external URL, missing credentials, non-https/link-local
  metadata URL.
- **OPS-01** — Leader election enabled (single active reconciler across
  replicas).

## Cross-cutting foundations to front-load with this first controller

Leader election (OPS-01), base Prometheus metrics (OBS-05), Secret
RBAC/informer-cache scoped to the auth namespace only (SEC-02), NetworkPolicy as
an owned resource alongside the Deployment/Service/ConfigMap (SEC-01), and CRD
CEL validation for **both** CRDs (SEC-03) — cheapest to wire correctly now;
every later slice inherits them.

## Carried blockers / concerns to resolve during the brainstorm

- **SSRF-guard timing (SEC-04, later slice, but decide the seam now):** is the
  IdP metadata-URL guard admission-time CEL only (https + not-link-local, no
  network fetch) or does the operator itself fetch? A well-precedented CVE
  class — resolve explicitly, not by convention.
- **`shibd` reload-vs-restart:** v1 defaults to always-roll gated by config-hash
  (DESIGN §11). Hot-reload is a deferred v1.x optimization (OPS-03), not a v1
  blocker.
- **NetworkPolicy enforcement is CNI-specific:** "the YAML exists" ≠ "the control
  is enforced." Plan an actual in-cluster verification against this cluster's CNI
  (Calico), not just manifest generation.

## Grounding references

- `DESIGN.md` §5–§7, §9, §11 — CRD shapes, operator design, sessions, gateway
  attachment, header hygiene.
- `docs/superpowers/archive/gsd-planning/REQUIREMENTS.md` — full requirement
  wording.
- `docs/superpowers/archive/gsd-planning/threads/saml-sp-operator.md` — spike
  fixes M/N/O and the learnings-to-carry-into-the-operator section.
- `internal/render/` — the Phase 1 render package this controller drives.
