# SPInstance Controller — Slice 1a: "one CR → running SP" — Design

**Date:** 2026-07-15
**Status:** Approved (design); pending implementation plan
**Slice:** 1a of the SPInstance controller (the first real feature slice through the superpowers pipe)
**Author:** Claude (brainstormed with Jeff)

## Goal

Applying a single `SPInstance` CR to the auth namespace produces a **running,
host-agnostic Shibboleth SP** — Deployment + ClusterIP Service + headless Service
+ ConfigMap — that loads cleanly and can span multiple app hosts. No
`AppIntegration` awareness yet.

Requirements: **SPI-01, SPI-02, SPI-03, SPI-07** + a **RENDER-02 revision**
(host-agnostic self-URL). Seeded from
`docs/superpowers/archive/gsd-planning/` via `spinstance-controller-seed.md`.

## Scope

The seed's "SPInstance controller" slice was split (brainstorm decision) into
**1a (this doc): a running SP**, and **1b: operator foundations** (memcached
SPI-05, NetworkPolicy SEC-01, RBAC/cache scoping SEC-02, CEL SEC-03, leader
election OPS-01, metrics OBS-05). 1b is a follow-on slice.

## The load-bearing decision: per-vhost multi-host (verified)

`SPInstance` must let **one SP span multiple app hosts** on standard `:443`
(Jeff's requirement). This was verified empirically this session, and it reversed
two earlier decisions — the evidence is why they changed.

### What was verified

A `shibdload`-tagged research spike (`internal/render/shibmultihost_test.go`)
booted the real pinned SP image with a **relative `handlerURL`**, two `<Host>`
RequestMap entries, and **only `SHIBSP_SERVER_SCHEME=https`** (no pinned
`SHIBSP_SERVER_NAME`), then asked the SP to build its ACS as reached on two
different hosts:

```
Host appa.example.com  →  ACS "https://appa.example.com/Shibboleth.sso/SAML2/POST"
Host appb.example.com  →  ACS "https://appb.example.com/Shibboleth.sso/SAML2/POST"
```

shibd loaded clean (no FATAL); each host got its own correct `https` ACS. So one
SP reconstructs the right per-host self-URL per request — multi-host is native
and needs *less* config than the spike, not more. The spike's earlier
"relative `handlerURL` breaks it" was a `:30443` non-standard-port
normalization artifact, absent on `:443`.

### Consequences (the two reversals)

1. **Phase 1's render is single-host and must be revised.** `selfurl.go` emits an
   *absolute, host-pinned* `handlerURL` and derives `SHIBSP_SERVER_NAME`/`PORT`
   from one `ExternalURL`. That contradicts multi-host.
2. **`externalURL` is not needed and is dropped.** We briefly decided to add
   `SPInstanceSpec.externalURL` (the SP's "front door"). A host-agnostic SP has
   no single front-door URL, so the field is removed from the plan; the CRD is
   unchanged.

### ACS model choice

**Per-vhost multi-host is the v1 model.** Its cost — the SP metadata is per-host
and re-registers with the IdP as app hosts are added (churn), and no cross-app
SSO at the SP layer — is accepted for v1. **Centralized ACS** (a dedicated auth
host + parent-domain cookie + RelayState return, which fixes the churn and adds
SSO) stays **deferred** (DESIGN §11); it is the unproven cross-host-cookie flow
the original spike deliberately avoided, and would need its own verification
spike before adoption.

## CRD

**Unchanged.** `SPInstanceSpec` stays exactly as scaffolded (`entityID`,
`credentials`, `idp`, `allowedNamespaces`, `sessionStore`). No `externalURL`, no
`acsHost`/`cookieDomain`. Status fields already scaffolded
(`conditions`/`observedGeneration`/`boundCount`/`metadataURL`/`configHash`); 1a
uses `conditions`/`observedGeneration`/`configHash` and leaves `boundCount`/
`metadataURL` for later slices.

## Render revision (RENDER-02, host-agnostic) — folded into 1a

The shared `internal/render` package changes so the rendered config is
multi-host-correct:

- **`shibboleth2.xml`**: `handlerURL` becomes **relative** (`/Shibboleth.sso`),
  not an absolute host-pinned URL.
- **`nginx.conf`**: the responder passes the **per-request host** (`$host`) with
  no port pin; standard `:443`; scheme forced by env, not a rendered value.
- **Self-URL derivation**: `SHIBSP_SERVER_SCHEME=https` is the only override
  (set as pod env, §Controller); `SHIBSP_SERVER_NAME`/`PORT` are **not** pinned.
- **`SPConfig.ExternalURL` is removed** — it drove the single-host derivation and
  has no remaining consumer (per-app `<Host>` scheme/port come from `AppBinding`,
  not the SP). `DeriveSelfURL` is removed or reduced accordingly.
- **Fixtures/tests**: golden fixtures and the `shibdload` test update to the new
  relative-handler output; `shibmultihost_test.go` is **kept** as a regression
  test proving the multi-host property (promoted from spike to committed test),
  **adapted to drive the revised `RenderShibboleth2` output** rather than its
  current hand-crafted fixture — so it tests real render output.

Non-standard external ports are **out of scope** for v1 (the cluster fronts on
standard `:443`; see the cluster-topology note). The port pinning that supported
`:30443` was a test artifact.

## Controller design (SPI-01/02/03/07)

### Reconcile flow

`Reconcile(SPInstance)`:
1. Fetch the `SPInstance`. (No finalizer — all owned objects are same-namespace,
   so `ownerRefs` handle GC. Cross-namespace finalizers are slice 5.)
2. Verify the `credentials` Secret exists in-namespace; if missing → set
   `Degraded`, requeue.
3. Build `render.SPConfig` from spec: `EntityID`, `IdP`, credential mount paths,
   in-memory `Sessions` defaults (memcached is 1b). No `ExternalURL`.
4. Render `shibboleth2.xml` (`RenderShibboleth2(cfg, nil)` — **empty RequestMap
   is valid**; a bare SP with no apps loads), `nginx.conf`, `attribute-map.xml`
   (empty). Compute `Hash(files)`.
5. Reconcile the four owned objects (below).
6. Set status.

### The four owned objects

- **ConfigMap** — the three rendered files.
- **Deployment** — the pinned SP image; mounts the ConfigMap at the shib config
  paths and the credential Secret at the keypair paths; sets
  `SHIBSP_SERVER_SCHEME=https` env; **config-hash annotation on the pod template**
  (SPI-02 — rolls only when rendered content changes); **`RollingUpdate` with
  `maxUnavailable: 0`** (SPI-07 — a config whose pod fails readiness can never
  retire a healthy pod); a real readiness probe (SPI-03, below). Single-pod
  supervisord process model (DESIGN §11's multi-container revisit is out of 1a).
- **ClusterIP Service** and **headless Service** (`clusterIP: None`) — the
  headless one is what forward-auth targets in later slices (spike fix O).
- Naming `<spinstance-name>-sp`; every object `ownerRef`'d to the `SPInstance`.

### Status

`configHash`, `observedGeneration`, conditions `ConfigRendered` (render
succeeded) and `Ready` (from Deployment availability); `Degraded` on missing
Secret or render failure. `boundCount`/`metadataURL` left for later slices.

## Readiness probe (SPI-03) — resolved at planning time

The spike's dumb `/healthz` 200 was insufficient (green even when shibd FATAL).
The real probe must prove shibd actually loaded. **Mechanism is a planning
research item**, tested against the image using the now-proven container harness
— leading candidate: exercise a shibd-backed handler (so a failed IdP-metadata
fetch → NotReady → `maxUnavailable:0` holds the last-good pod). Not hand-waved;
pinned down in the plan with a container test.

## SP image sourcing

An **operator-global flag** `--sp-image` (default = a pinned digest, matching
Phase 1's `shibdload` pin), not a per-`SPInstance` field — every SP runs the
operator-matched image. Rationale: the SP image is an operator-version-coupled
artifact, not per-tenant config.

## Out of scope / deferred

- **1b (next slice):** memcached sessions (SPI-05), NetworkPolicy (SEC-01),
  RBAC/informer-cache namespace scoping (SEC-02), CEL validation (SEC-03), leader
  election (OPS-01), Prometheus counters (OBS-05).
- **OBS-03 (metadata URL):** per-vhost metadata is per-host and only meaningful
  once app hosts exist — deferred to a slice where the per-host metadata story is
  surfaced.
- **Centralized ACS:** deferred (DESIGN §11); needs a cross-host-cookie
  verification spike before adoption.
- **Non-standard external ports; multi-container pod model.**

## Testing strategy

- **envtest** — the reconcile logic: applying an `SPInstance` creates the four
  objects with correct ownership; config-hash gating (unrelated edits don't roll,
  content edits do); status transitions; `Degraded` on missing Secret.
- **Container harness** (the proven `shibdload`/`shibmultihost` pattern) — that
  the rendered config loads in a real shibd and spans hosts; candidate readiness
  probes.
- A **live cluster / kind** validates the actually-running SP end-to-end (envtest
  cannot run shibd).

## Verification (1a is done when)

1. Applying an `SPInstance` (valid `credentials` Secret present) yields a running
   Deployment + ClusterIP Service + headless Service + ConfigMap, all
   `ownerRef`'d, with `Ready=True`.
2. Editing a field that doesn't change rendered config does **not** roll the
   Deployment; editing `entityID`/credentials does, gated by the config-hash
   annotation.
3. Feeding a spec that produces invalid config (or killing shibd) flips readiness
   to NotReady; with `maxUnavailable:0` the rollout halts and the last-good pod
   keeps serving.
4. The rendered SP is **host-agnostic**: `shibmultihost_test.go` passes (one SP,
   correct per-host ACS for multiple hosts), and the render package no longer
   emits an absolute host-pinned `handlerURL`.
5. A missing `credentials` Secret surfaces `Degraded` with a human-readable
   reason, not a crash.

## Open items for the plan

- The exact readiness-probe mechanism (research against the image).
- Whether removing `SPConfig.ExternalURL` ripples into any Phase 1 caller not yet
  surfaced (the render package is currently the only consumer; confirm during
  planning).
