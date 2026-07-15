# Phase 1: Shared Render & Aggregation Package - Context

**Gathered:** 2026-07-10 (assumptions mode)
**Status:** Ready for planning

<domain>
## Phase Boundary

A pure Go package (`internal/render`) that turns SP identity + resolved app
bindings into byte-correct, injection-safe Shibboleth SP v3 config
(`shibboleth2.xml`, `attribute-map.xml`, `nginx.conf`) plus the RequestMap
collision resolution — with **zero Kubernetes runtime dependency**. Both
controllers will later import this same package so they can never disagree about
a collision winner.

**In scope:** RENDER-01 … RENDER-10 — the rendering functions, the deterministic
`Resolve`/collision logic, the config hash, injection safety, and the two-layer
test strategy (golden + real-shibd load).

**Out of scope:** any controller, reconciler, k8s client, CRD-field wiring, the
Deployment/rollout mechanics (Phase 2), and the standalone single-container
consumer (future, not v1). This phase writes a library, not an operator.
</domain>

<decisions>
## Implementation Decisions

### Package API shape — pure Go library
- **D-01:** `internal/render` takes **plain-Go input structs** (e.g. `render.SPConfig`,
  `render.AppBinding`) that the controllers populate — it does **not** import
  `api/v1alpha1` or `k8s.io/apimachinery`. The RequestMap's `(hostname, path)`
  data does not exist on the CRD spec at all (it is resolved from the HTTPRoute
  by the controller), and `createdAt`/UID live on `ObjectMeta`, not spec — so the
  package physically needs a synthesized input regardless of the dependency
  question.
- **D-02:** The "no k8s dependency" line is deliberate, not arbitrary purity: the
  render core is the shared seam between this operator (Traefik ForwardAuth
  attachment) and a planned standalone single-container deployment (nginx
  `auth_request` attachment). Keeping it k8s-free lets both consume it without a
  base-container merge that would be wrong. RENDER-08's per-attachment-model
  clear-list already encodes the two-consumer reality. See PROJECT.md Key
  Decisions.

### XML/config generation mechanism
- **D-03:** `shibboleth2.xml` + `attribute-map.xml` are produced by `encoding/xml`
  struct marshaling (RENDER-01), using the **literal-`xmlns`-attribute approach**:
  declare `xmlns` and `xmlns:conf` as plain string attributes on the **root
  struct only** (`xml:"xmlns,attr"`, `xml:"xmlns:conf,attr"`), with every child
  element carrying a bare local-name tag (empty `Space`). A live Go 1.26
  experiment confirmed this yields the target fixtures byte-for-byte; the
  namespace-aware `xml.Name{Space}` approach re-declares `xmlns` on every child
  (Go issue #9519, unresolved) and must not be used. Do not mix the two styles.
- **D-04:** `nginx.conf` is rendered via `text/template` (RENDER-07). The `<Host>`
  entries and `SHIBSP_SERVER_{NAME,SCHEME,PORT}`/absolute `handlerURL` self-URL
  derivation honor spike fixes M and N (explicit `scheme`+`port` on every
  `<Host>`, fully-qualified handlerURL) — the gate fails **open** otherwise.
- **D-05:** Injection safety (RENDER-10) rides on `encoding/xml`'s automatic
  escaping of element/attribute values, **plus an explicit `--` guard** on any
  CRD-derived string routed into an XML comment (spike fix K). The real failure
  mode is not FATAL XML: Go's marshaler *refuses to emit and returns an error*
  on any `--` inside a comment, so an unsanitized hostile/odd value would break
  config generation entirely. Strip at the input layer
  (`strings.ReplaceAll(v, "--", "-")`) before marshaling.

### Collision resolution & config hash
- **D-06:** Split **`Resolve(bindings) → Resolution{ Winners, Conflicts[] }`** (pure
  collision logic) from **rendering** (consumes winners). `Conflicts` carries
  `{ Winner, Loser (ns/name/UID), Hostname, Path }`. `Resolve` is exported and
  callable independently so the AppIntegration controller (APP-04) computes its
  **own** `Conflict` condition over its sibling list — never a cross-controller
  status write. This decomposition IS the "both controllers can never disagree"
  guarantee (they run the same `Resolve`).
- **D-07:** Deterministic sort key: **`(priority desc, createdAt asc, UID asc)`**
  (RENDER-06). New `AppIntegration.spec.priority` (int32, higher wins, default 0)
  is consulted first, then oldest `createdAt`, then UID as final tiebreak. The
  UID tiebreak is load-bearing, not cosmetic: `metav1.Time` is second-granular,
  so same-second creations tie on `createdAt` and MUST fall through to UID for
  determinism. Test the same-second case explicitly. `AppBinding` carries
  `Priority int32`; the CRD field lands when the AppIntegration controller maps
  CRD→`AppBinding` (Phase 3/5) or opportunistically earlier for API stability —
  planner's call.
- **D-08:** Config hash = one `sha256` over a stable, delimited concatenation of
  `{filename, bytes}` for **all rendered pod-config files: `shibboleth2.xml` +
  `nginx.conf` + `attribute-map.xml`** (RENDER-09). `attribute-map.xml` is
  `reloadChanges="false"` (spike shibboleth2.xml:98) — shibd never hot-reloads
  it, so an attribute-only change must force a pod roll; excluding it from the
  hash is a silent correctness bug (the scaffold's `configHash` doc comment says
  only "shibboleth2.xml + nginx.conf" and is incomplete — fix it when Phase 2
  touches it). The RequestMap is inline in shibboleth2.xml, already covered.
  `attribute-policy.xml`/`protocols.xml` are static image files in v1, not
  rendered, so out of hash scope. Deterministic render (D-07) is what makes the
  hash reorder-stable without a separate canonicalization pass.

### Testing strategy
- **D-09:** Two layers: (1) golden-file byte-compare of rendered artifacts against
  the spike fixtures; (2) a **build-tag/env-gated real-`shibd` container load
  test** that mounts the rendered `shibboleth2.xml` into a real shibd (the way
  `edge/testenv/docker-compose.yml` already does) and asserts it parses/loads —
  satisfying Phase 1 success criterion #1 ("not just a golden-file text-compare").
  Plain `go test` stays hermetic; the load test is the gated layer. The load test
  is also the **build-time net** of the fail-safe-rollout guarantee (below).
- **D-10:** CI feasibility confirmed: the SP image is a **public** GHCR package
  (`ghcr.io/jtickle/saml-sp-operator/shib-authenticator`) and `ubuntu-latest` has
  Docker, so the containerized load test is runnable in public-repo Actions. The
  load test image must stay publicly pullable (no employer/private infra).

### Cross-cutting guarantees this phase feeds
- **D-11:** **Fail-safe rollout** (the ingress-nginx property — a config push can
  never break a running SP) is a hard project guarantee with three independent
  nets: (1) build-time — D-09's load test proves valid input → shibd-loadable
  output before anything ships; (2) admission-time — CEL rejects malformed specs
  (SEC-03, Phase 2); (3) runtime — readiness proves shibd loaded (SPI-03) +
  Deployment `RollingUpdate maxUnavailable: 0` (**SPI-07**, added this phase's
  discussion, mapped to Phase 2). Phase 1 owns net (1). See PROJECT.md Key
  Decisions and REQUIREMENTS.md SPI-07.

### Claude's Discretion
- Exact Go struct layout, function/file names, and package sub-structure within
  `internal/render` (as long as `Resolve` is independently exported per D-06).
- Golden-fixture directory layout and the specific build-tag/env-var gating the
  load test.
- Whether the CRD `priority` field is added now vs at controller-wiring time
  (D-07).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

- `DESIGN.md` §2 (own-orchestration/borrow-SAML, the `Variable-` header-prefix
  leak), §6 (request mapping / RequestMap), §7 (operator design, config-hash
  rollout, status), §10 (spike hypotheses)
- `.planning/REQUIREMENTS.md` — RENDER-01 … RENDER-10 (exact wording; RENDER-06
  updated with the `priority` sort key), and SPI-07 (fail-safe rollout)
- `.planning/ROADMAP.md` — Phase 1 section (goal + 5 success criteria)
- `.planning/PROJECT.md` — Key Decisions table (render-core-is-k8s-free;
  fail-safe rollout)

**Golden fixtures the render package must reproduce (on branch `spike`, repo
root):**
- `shibboleth2.xml` — the proven SP config; inline comments document spike fixes
  M, N, O, K and the `reloadChanges="false"` on `attribute-map.xml` (line 98)
- `attribute-map.xml` — SAML attribute id → `Variable-<id>` mapping
- `nginx.conf` — X-Forwarded-* → FastCGI params, `/authcheck` + `/Shibboleth.sso`
- `edge/testenv/` — working docker-compose harness (traefik + real SP + IdP);
  `edge/testenv/docker-compose.yml` is the load-test harness reference
- CRD types (on branch `gsd/operator-scaffold`): `api/v1alpha1/spinstance_types.go`,
  `api/v1alpha1/appintegration_types.go`
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The **spike fixtures** at repo root (`shibboleth2.xml`, `attribute-map.xml`,
  `nginx.conf`) are proven-correct golden targets — the render package's job is
  to regenerate exactly these from structured input.
- `edge/testenv/docker-compose.yml` already stands up a real shibd container
  mounting exactly these files — the load-test harness exists; reuse its shape.
- The **CRD types** on `gsd/operator-scaffold` (`SPInstanceSpec`,
  `AppIntegrationSpec`, `AttributeMapping`, etc.) define the semantic vocabulary
  the plain-Go render inputs mirror (without importing them).

### Established Patterns
- Go 1.26, controller-runtime v0.24.1, apimachinery v0.36.0 (scaffold `go.mod`) —
  but `internal/render` imports **none** of the k8s ones (D-01).
- Spike fixes are non-negotiable render invariants: explicit `scheme`+`port` on
  every `<Host>` (N), fully-qualified `handlerURL` + `SHIBSP_SERVER_*` (M),
  headless-Service targeting (O — a controller concern, not render), no `--` in
  comments (K).

### Integration Points
- `Resolve` is the shared collision function both controllers import (Phase 3/4/5).
- The config hash (D-08) is consumed by the SPInstance controller's pod-template
  annotation for hash-gated rollout (SPI-02, Phase 2).
- The per-attachment-model clear-list (RENDER-08) is where the two-consumer
  (Traefik vs nginx) reality surfaces in the render output.
</code_context>

<specifics>
## Specific Ideas

- **`priority` field, higher-wins, default 0** (D-07) — Jeff's addition during
  discussion; k8s `PriorityClass` idiom, backward-compatible with pure oldest-wins.
- **Security note (for the planner):** `priority` is self-asserted and
  AppIntegrations are cross-namespace, but it is **not** an escalation — two teams
  can only collide on `(hostname, path)` if both legitimately have HTTPRoutes
  attached to that hostname, and Gateway API already arbitrates hostname
  ownership at route attachment. `priority` decides precedence *among
  already-authorized routes*; it cannot steal a hostname a team was never allowed
  to serve. Write this into the AppIntegration controller's threat model so it is
  not later mistaken for a trust hole.
- **`maxUnavailable: 0`** must be the explicit Deployment rollout strategy in
  Phase 2 (SPI-07) — the mechanism that makes the fail-safe guarantee airtight.
</specifics>

<deferred>
## Deferred Ideas

- **Standalone single-container deployment tool** (nginx `auth_request` consumer
  of the render core) — future product surface, not v1. Phase 1 only preserves
  the option by keeping the package k8s-free; it does not build the tool.
- **`priority` as a documented v2 knob** — if oldest-wins + the field prove
  insufficient, richer precedence policy is a v2 concern.
- **`shibd` hot-reload for RequestMap edits** (OPS-03, v2) — v1 always rolls,
  gated by config-hash; no hot-reload path in the render package.

### Reviewed Todos (not folded)
None — no pending todos matched this phase.
</deferred>
