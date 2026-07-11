# SAML SP Operator — Design & Decision Record

A self-hosted, Kubernetes-native SAML Service Provider for protecting apps
behind Gateway API, built as an operator over a containerized Shibboleth SP.
This document captures the design we worked through and *why* each call was
made — including the paths we rejected, which are as load-bearing as the ones
we kept.

---

## 1. Problem & context

- Need zero-trust-style authentication middleware in front of apps on
  **Kubernetes Gateway API**, with **native SAML** (no OIDC broker/wrapper).
- Currently on **Traefik**, possibly moving to **Cilium** — so nothing
  implementation-specific may leak into the durable design.
- Prior production reality: a containerized Shibboleth SP behind
  **ingress-nginx**, using `shibauthorizer` as the `auth_request` backend.
  Crude (whole `shibboleth2.xml` in a ConfigMap, certs from a Secret) but it
  worked — except it spun up **many copies** of the SP.
- **ingress-nginx reached EOL on 2026-03-24** (read-only repo, no more CVE
  patches; EOL L7 components trigger SOC2/PCI/ISO audit findings), forcing the
  migration off it.
- Apps were temporarily fronted by a hosted zero-trust access proxy, which
  handled SAML — until projected usage made its cost model untenable, pointing
  back to self-hosting in Kubernetes.

**Goal:** the operator + few-instances + shared-session-store model originally
wanted, but now gateway-portable and never welded to an ingress controller again.

---

## 2. The core architectural decision: own the orchestration, borrow the SAML

The single most important framing. The durable asset is the **orchestration** —
the CRDs, the operator, the Gateway API attachment, the session model. The
**SAML engine underneath is a swappable backend**. We design an abstraction
boundary so the engine can be replaced without touching the operator/CRDs.

**Known leak in that boundary (the one to call out):** the *naming* of the
exported identity headers is engine-determined, not operator-controlled — SP3's
FastCGI authorizer hard-codes a `Variable-` prefix, and a different engine (SP4, a
non-Shibboleth SP, a future Hub/Agent) may emit different names. So swapping the
engine is a header-contract-affecting change. Everything else the operator
generates is engine-agnostic; header naming is the sole exception. Locked and
detailed in §9 addendum (2026-07-09).

### Why not write our own SAML core (in Rust)?
- The value is in orchestration, none of which is cryptographic.
- The SAML core rests on **XML signature validation**, a minefield: XML
  Signature Wrapping (XSW), C14N/transform handling, comment-truncation,
  `KeyInfo` trust confusion, algorithm downgrade. A subtle bug is a **silent,
  total auth bypass** with no error and no log.
- The Rust ecosystem forces the issue anyway: `samael` is the only viable
  crate, still **0.0.x**, and its signature verification isn't even pure Rust —
  it binds to the C `xmlsec1` library, dragging in libxml2/libxslt/libclang.
  So you escape neither C deps nor an unaudited glue layer for the most
  bypass-prone code in the system.
- **Verdict:** write our own SP *orchestration* — yes, our wheelhouse. Write
  our own SP *SAML core* — no. Borrow a vetted engine.

### Why Shibboleth as the engine?
- Battle-tested, InCommon-grade, the most-scrutinized open-source SAML in higher
  ed; we already trust it.
- We get full-spec SAML including SLO without writing XML-DSIG ourselves.

---

## 3. Shibboleth: which generation? (And why not the shiny new one)

We investigated Shibboleth's **Hub & Agents** redesign (the SP v3 successor):

- **The Hub** is the `shibd` successor — but it's implemented as **plugins for
  the Shibboleth IdP and requires the Java IdP runtime (≥ v5.2)**. So "thin
  wrapper over the SP" now means operating a heavyweight Java/Spring IdP-based
  service in-cluster.
- **The Agents** are the web-server-module successor: thin request-path
  components that **remote** SAML operations to the Hub over a **DDF
  (Dynamic DataFlow)** RPC protocol, HTTP-framed.
- Notably, the Shibboleth architects **explicitly rejected the ext_authz
  "replay the whole HTTP request" model** (the very model GEP-1494/forward-auth
  is built on) in favor of DDF-RPC, because not all SP operations map to
  user-agent requests.

**Why we're not building on it (yet):** it's draft and brand-new (Agents
v4.0.0 with no prior stable release, Hub v1.0, the remoting protocol still a
DRAFT), and adopting it means running an IdP-Java Hub *and* having our operator
reconcile CRDs into **Hub config** rather than our own model. It also is, in
spirit, exactly the heavyweight "broker you operate" we wanted to avoid. We
traded the old SP's "too late (EOL)" for the new one's "too early."

**Decision:** ship on **containerized Shibboleth SP v3** now; treat Hub/Agents
as the future engine to swap in as it matures. Our Agent-shaped adapter pod
(below) is conceptually the same role the future Shibboleth Agent will play, so
the topology transfers.

> SP v3 is **not** currently EOL — it's the terminal C++ release, still
> supported, no published imminent cutoff. Shipping on it this month is fine.
> (Its weight history: the old SP's "right size" was held up by a *second*,
> hand-maintained C++ SAML stack — cpp-opensaml/cpp-xmltooling — being retired.
> The Hub got "heavy" by collapsing that duplication into the Java OpenSAML the
> IdP already maintains. The weight was relocated and de-duplicated, not added.)

---

## 4. The encapsulation trick (the heart of the gateway-portability fix)

`shibauthorizer`/`shibresponder` are **FastCGI** apps. `nginx auth_request` and
Apache can drive FastCGI; **Traefik ForwardAuth, Envoy/Cilium ext_authz, and
GEP-1494 ExternalAuth speak HTTP/gRPC, not FastCGI.** *That* is why the
shibauthorizer integration is "proven against nginx but not other servers" — not
flakiness, but a protocol mismatch.

**Resolution: don't port it — encapsulate it.** Put the proven
`nginx + shibauthorizer + shibresponder + shibd` assembly in **one pod**, and
have the *internal* nginx expose plain HTTP. nginx moves from "cluster ingress"
to "in-pod FastCGI→HTTP adapter."

Crucial distinction: what reached EOL is the **ingress-nginx controller**, not
**nginx the web server**. The encapsulated adapter is stock, maintained nginx,
behind the real gateway, not internet-facing.

```
[Traefik / Cilium gateway]        internet-facing, maintained, Gateway API
   │  (1) HTTP forward-auth/ext_authz  →  /authcheck
   │  (2) normal HTTPRoute             →  /Shibboleth.sso/*
   ▼
[Authenticator Pod]               few of these, operator-managed
   nginx (stock, internal)
        ├─ fastcgi → shibauthorizer  (the /authcheck verdict)
        └─ fastcgi → shibresponder   (ACS, Login, Logout, Metadata)
   shibd ── memcached/ODBC ──→ [shared session store]
```

A subtlety the spike validates: nginx needs no special module because **Traefik
ForwardAuth already provides the FastCGI-authorizer semantics** (copy
`authResponseHeaders` on allow; return the auth response — including the 302 to
the IdP — to the browser on deny). nginx just relays the authorizer's raw
response. The one real wiring detail is mapping the gateway's `X-Forwarded-*`
headers into the FastCGI params so the SP's RequestMapper keys on the **real**
request URL, not on `/authcheck`.

---

## 5. The CRD model

Two CRDs, split along a real SAML truth: entity-ID/keypair/IdP-trust are
properties of the **SP entity**, while ACS is just one binding that entity
advertises (an SP may advertise many).

- **`SPInstance`** — lives in the **auth namespace**. Holds entityID, the
  keypair Secret, IdP/federation config (single IdP or a signed metadata feed +
  verification cert), an `allowedNamespaces` consent selector, and the session-
  store reference.
- **`AppIntegration`** — lives in the **app namespace, beside the HTTPRoute**.
  `targetRef`s the app's HTTPRoute, carries ACS/per-app settings, attribute→
  header mapping, per-app authz, session policy, and the SLO opt-out.

**Cardinality is the shared-vs-dedicated fork, left open by the model:** many
`AppIntegration` → one `SPInstance` = shared multi-tenant authenticator (one
entityID, cross-app SSO); 1:1 = dedicated. We never choose in the schema; the
operator reconciles whatever graph it's handed.

### Namespace rules (these are mandated, not stylistic)
- `AppIntegration` **must** be same-namespace as the HTTPRoute it targets:
  Gateway API policy attachment is namespace-local by design (a `targetRef` has
  no namespace field, deliberately — cross-namespace attachment without consent
  is a security hole).
- `SPInstance` lives central in the auth namespace so the SP **private key is
  not readable from every tenant namespace**.
- The cross-namespace hop that *does* need a **ReferenceGrant** (in the auth
  namespace) is the data-plane `backendRef` from the generated attachment to the
  authenticator Service — and only for attachment types that use a real
  backendRef. Traefik's ForwardAuth-by-URL sidesteps ReferenceGrant entirely.
- `SPInstance` needs an `allowedNamespaces`/label-selector **consent**
  mechanism so an app team can't bind to another tenant's federation trust by
  name.

### The Shibboleth guidance that shaped the model
Do **not** mint an entityID or an `ApplicationOverride` per app by default —
that's an anti-pattern that interoperates poorly with federations. Use **one
entityID for the whole deployment**; differentiate apps by per-path **content
settings + AccessControl**, not separate applications. `ApplicationOverride` is
the rare escape hatch (a pinned IdP, a distinct attribute policy), and when
used, must be minimal (`<ApplicationOverride id="x" entityID="…"/>`), with its
handler path mapped back to the same applicationId or you hit
looping/invalid-audience errors. This firmly resolves the fork toward **shared**.

---

## 6. Request → application mapping (host/path, derivable from the HTTPRoute)

The native **RequestMapper** does host/path (and query) → `applicationId` via
ordered `<Host>`/`<HostRegex>` and nested `<Path>` elements — exactly what an
HTTPRoute already declares. The operator resolves the targeted HTTPRoute, reads
`spec.hostnames` and `spec.rules[].matches[].path`, and emits the RequestMap
plus AccessControl. Boundary: RequestMap sees host/path/query only —
header/method-based app separation in an HTTPRoute is **not** derivable and
should surface as a `Degraded` condition.

**Do we need anything beyond stock nginx + the Shibboleth FastCGI?** No. The
only "addition" is operator-generated nginx config — chiefly the `X-Forwarded-*`
→ FastCGI-param mapping plus the `/authcheck` and `/Shibboleth.sso/*` locations.
No custom module for the host/path case. (Security: the authenticator Service
must be reachable **only** from the gateway via NetworkPolicy, because
applicationId/authz now depend on `X-Forwarded-Host`, which is otherwise
client-spoofable; and the gateway must strip client-supplied identity/forwarded
headers.)

---

## 7. Operator design (Go / kubebuilder)

Go is the clear choice — and an easy one, because choosing Shibboleth as the
engine drained all the crypto out of the operator. It does **zero SAML**: it
watches CRDs + HTTPRoutes, renders `shibboleth2.xml` + `nginx.conf` into a
ConfigMap, rolls/reloads the Deployment, and emits the gateway attachment +
ReferenceGrant. Textbook controller-runtime.

### Two controllers, not one (different lifecycles, different namespaces)
- **AppIntegration controller** (app namespaces): `For(AppIntegration)`,
  `Owns(Middleware)` (same-ns attachment), `Watches(HTTPRoute, SPInstance)`.
  Validates consent, resolves the route, renders the same-ns attachment.
- **SPInstance controller** (auth namespace): `For(SPInstance)`,
  `Owns(Deployment, ConfigMap, Service, ReferenceGrant)`,
  `Watches(AppIntegration, Secret)`. Lists all bound AppIntegrations, aggregates
  their fragments into one ordered `shibboleth2.xml`, rolls on config-hash change.

### The cross-namespace ownerRef trap
Owner references are namespace-local; a namespaced object can't own one in
another namespace and GC silently won't fire. So ownership is **split by
namespace**, and everything cross-namespace is coordinated by **finalizers**,
not ownerRefs (the AppIntegration finalizer maintains its entry in the auth-ns
ReferenceGrant's `from` list and triggers re-render on delete). Set up field
indexes on `.spec.spInstanceRef` and `.spec.targetRef.name` for the fan-out map
functions.

### RequestMap aggregation & ordering
1. Collect `(hostname, path, appId, settings, ns, createdAt)` from resolved
   routes; skip header/method-only matches → `Degraded`.
2. **Collision** on `(hostname, path)` → deterministic winner = oldest
   `createdAt` (UID tiebreak); loser gets a `Conflict` condition + Event and is
   **excluded** from the render. Never last-write-wins.
3. Partition hostnames: exact → `<Host>`, wildcard → `<HostRegex>` (the SP
   matches all `<Host>` first, then `<HostRegex>`, so exact beats wildcard for
   free — matching HTTPRoute precedence).
4. Group by hostname, order paths most-specific-first; build nested `<Path>`
   from segments so overlapping prefixes nest correctly.
5. Serialize, hash, stamp as a pod-template annotation; only roll when the hash
   changes (otherwise unrelated AppIntegration reconciles churn the fleet).

### Rollout & status
- Rolling restart is safe **because** sessions live in shared memcached — a
  malformed config fails the new pod's readiness, the old pod keeps serving, and
  the rollout halts instead of taking auth down fleet-wide. Readiness must
  *prove the SP loaded* (hit a responder handler), not just TCP.
- Status: `SPInstanceResolved`/`RouteResolved`/`Conflict`/`Degraded`/`Ready` on
  AppIntegration; `ConfigRendered`/rollout health/bound-count and the generated
  **SP metadata URL** on SPInstance (hand it to IdP admins straight from status).
- Investigate **config reload vs pod roll**: if shibd hot-reloads RequestMap
  changes, routine AppIntegration edits can signal a reload instead of rolling.

---

## 8. Sessions, SLO, and the shared store

- **Store:** shibd's default cache is in-process; multi-replica needs a
  clustered StorageService — **memcached** (the "shared memory store") or ODBC.
- **HA is a UX choice, not a correctness one:** a lost session is just a re-SSO
  bounce, invisible while the IdP session lives. So a single **persistent**
  (non-HA) store is the sweet spot — survives pod rollouts and its own restarts;
  full HA memcached is a premium tier, not a baseline. The one real exception is
  a mid-POST re-auth: the redirect is a GET, so the POST body is lost — minimize
  *frequency* (persistent store + generous TTLs) rather than trying to make it
  survivable; every forward-auth setup loses POST bodies on re-auth.
- **Session schema is driven by SLO:** IdP-initiated logout references the
  session by `(NameID, SessionIndex)`, not your cookie, so the store must be
  indexed by those as well as the cookie ID. Add a `user → sessions` secondary
  index for free force-logout/audit and an optional app-queryable session API
  (with its own authz: an app sees only its own SP/tenant; a user only their own).
- **SLO is best-effort, and made optional per CRD.** Real IdPs spool SPs and
  fail them independently (the strict blocking-chain is a worst case). When SLO
  is off, the SP **must not advertise an SLO endpoint in its metadata**, and
  `/__auth/logout` degrades to local-only (drop cookie + session; IdP session
  lives on → seamless re-SSO).
- **Well-known URLs:** an operator-owned reserved prefix (e.g. `/__auth/*`) for
  `logout`/`whoami`, uniform across hosts so apps have one stable contract and
  never know SAML exists.

---

## 9. Gateway API attachment & hostname ownership

### Attaching auth without the app ceding its route
- The clean ideal is **policy attachment** (`targetRef`, no route edit) — which
  is exactly what **GEP-1494's two-part design** (the ExternalAuth filter, then
  a Policy object targeting Gateway/HTTPRoute) is for. But the Policy half
  hasn't shipped, and it's Envoy-protocol-based and still **Experimental**.
- **On Traefik specifically**, the only Gateway API attachment is an inline
  `ExtensionRef` filter on the HTTPRoute pointing at a `traefik.io` Middleware,
  and that ExtensionRef is a `LocalObjectReference` (Middleware must be
  same-namespace; Traefik hasn't extended ReferenceGrant to cover it). Requires
  the `kubernetesCRD` provider enabled.
- **Decision:** the app author adds **one `ExtensionRef` filter line** to their
  own route, referencing an **operator-managed Middleware**. The app keeps full
  ownership and GitOps-safety (each side owns its own manifest); the operator
  supplies what the filter does. When GEP-1494 Policy lands (or on Cilium via
  SecurityPolicy's `targetRef`), the operator switches spelling and the app
  drops the line — the CRD is unchanged. (Operator co-editing the app's route is
  rejected: GitOps reverts it.)

### Hostname-claim protection (preventing accidental/hostile host collisions)
- Gateway API's **default is additive-merge with deterministic conflict
  resolution** (oldest `creationTimestamp` wins) — so without gating, a second
  party *can* claim a hostname and the loser silently half-breaks.
- Native mechanisms, by scale:
  - `allowedRoutes` (namespace selector on `kubernetes.io/metadata.name`) — the
    always-on "who may attach" baseline.
  - **Listener-per-hostname + allowedRoutes** — true per-namespace hostname
    ownership, but capped at **64 listeners** per Gateway; wrong tool for a SaaS.
  - **ValidatingAdmissionPolicy** (CEL) — the scalable answer: a per-namespace
    domain-allowlist annotation, validated against every route's
    `spec.hostnames` at admission, no webhook, scales arbitrarily. Anchor on
    `kubernetes.io/metadata.name` (other labels are spoofable).
- **Decision (single-team cluster):** ship an **opt-in** VAP — enforcement
  triggers *only* when a namespace carries the domain annotation, so it's a
  no-op until you opt in — **plus a byte-identical check in the operator** so the
  same rule holds even without the VAP installed (clean `Accepted=False /
  HostnameNotAllowed` status + exclusion from the render). Defense in depth: VAP
  prevents at admission, operator resolves residual collisions at reconcile. The
  ready-to-use VAP + binding + matching Go were drafted in-thread.

### Addendum (2026-07-09): ext-authz standard status, the 302-relay unknown, endpoint-vs-ClusterIP

Web research + the in-cluster spike sharpened §9's swappability story into three
concrete decision inputs:

- **The GEP-1494 `ExternalAuth` *filter* has now shipped on Cilium and Envoy
  Gateway** (Phase 1 — the filter; Phase 2 Policy defaults still pending), and its
  **HTTP mode is a near-exact match for our SP edge**: auth server returns `200`
  → allow + copy `AllowedResponseHeaders` upstream (our `Variable-*`/`Remote-User`
  → `authResponseHeaders`, 1:1); non-200 → deny. It references the auth service by
  **`backendRef`**, and Envoy Gateway's `SecurityPolicy.extAuth` likewise. This is
  the portable, Traefik-agnostic target §9 wanted, now real enough to design
  toward (still **Experimental** — target, don't depend). The SP image is
  unchanged across Traefik `ForwardAuth` / Envoy `SecurityPolicy` / GEP-1494
  `ExternalAuth`; **only the attachment CRD is swappable** — that's the boundary.

- **THE load-bearing portability unknown: does each dataplane relay the SP's
  302-to-IdP?** Interactive SAML needs an unauthenticated hit to bounce the browser
  to the IdP. Traefik `ForwardAuth` relays *any* non-2xx (incl. 302) verbatim — so
  it works, and it's exactly why the `ngx_http_shibboleth` module exists (native
  nginx `auth_request` treats 302 as an error). Envoy `ext_authz` *can* return a
  denied response carrying a 302 + headers, but **GEP-1494's HTTP mode as written
  says "non-200 = failure" and does not specify redirect relay.** So "ext-authz
  everywhere" is unproven for the interactive-login leg and **must be spiked per
  dataplane** (Cilium, Envoy Gateway). If a gateway won't relay the 302, the
  fallback for that gateway is the nginx-`auth_request`+module edge (the model that product
  proved) fronting it. This decides whether the operator can be gateway-native
  everywhere or must ship the nginx-module edge for some dataplanes.

- **The ForwardAuth address must resolve to pod ENDPOINTS, not the ClusterIP**
  (spike fix O). Traefik `ForwardAuth` dials a **URL string** → the Service
  **ClusterIP**, and that dial fails with `network is unreachable` while the
  Gateway's own routing (which resolves to pod endpoints) works fine. **Root cause
  corrected 2026-07-09 from the live Calico policy:** it is NOT an egress-policy
  block — this cluster's default-deny `GlobalNetworkPolicy`
  **exempts the `traefik` namespace** (and all infra namespaces) from the
  default-deny, so gateway egress is unrestricted. `ENETUNREACH` to the ClusterIP
  with egress open is a **kube-proxy / Service-VIP resolution** matter (the VIP
  isn't DNAT'd for the gateway pod's connection), i.e. a routing-layer / CNI
  concern, not a NetworkPolicy denial. That makes the lesson *stronger*: even with
  gateway egress fully open, **gateway→ClusterIP reachability is a fragile,
  CNI/kube-proxy-specific assumption** — and the target spans Calico (current) →
  Cilium (future) → Gateway-API-in-general, each handling Service-VIP-from-a-pod
  differently. So the operator must target **endpoints**, not the ClusterIP.
  Fix: a **headless Service** (`clusterIP: None`) so the FQDN yields endpoint IPs.
  A headless Service *is* a Service (name, selector, EndpointSlice intact); it just
  omits the kube-proxy VIP — this is the gateway norm (ingress/Gateway dataplanes
  load-balance to endpoints, not the ClusterIP), not a workaround. It aligns with
  the standard: **GEP-1494/Envoy `backendRef` resolve to endpoints** (via
  EndpointSlice tracking), which sidesteps the ClusterIP path entirely **and**
  fixes headless's one weakness — DNS staleness when a pod is recreated with a new
  IP (headless returns pod A-records; a client caching the old one dials a dead IP
  until re-resolve/TTL; the ClusterIP VIP is stable across recreation, but is
  unreachable here anyway; `backendRef` endpoint-tracking has neither problem).

- **LOCKED: exported header *naming* is engine-determined, not operator-controlled
  — the one place the swappable-engine boundary (§2) leaks into the app-facing
  contract.** The SP3 FastCGI `shibauthorizer` hard-codes a `Variable-` prefix on
  every exported header — `Variable-REMOTE_USER`, `Variable-<attr-id>` (confirmed:
  the string `Variable-` is a literal in the `shibauthorizer` binary, absent from
  `libshibsp`; there is **no `shibboleth2.xml` knob** for it, and the Apache-module
  export controls `ShibUseHeaders`/`spoofKey`/`useHeaders` aren't even in that code
  path). The operator controls the **suffix** (attribute-map `id`) and *which*
  attributes export, but **not the prefix or the emitted shape** — those belong to
  the engine binary. Everything else the operator generates (external-URL env,
  RequestMap, ForwardAuth wiring, attribute *mapping*) is engine-agnostic config we
  fully own; **header naming is the sole engine-implementation-dependent element of
  the identity contract.** Consequences, locked:
  - **Swapping the SAML engine is a header-contract-affecting change.** SP3 →
    SP4 → a non-Shibboleth SP → a future Hub/Agent engine may emit different names
    (SP4 is a ground-up rewrite; the FastCGI-authorizer + `Variable-` convention
    likely will not carry over — Jeff's expectation is this constraint *goes away*
    at SP4). Each engine adapter must **document its emitted header naming** as part
    of its contract, and the operator must surface it as engine-determined rather
    than promise a stable cross-engine app-facing naming.
  - **The rename layer, where one exists, is the shock absorber.** nginx
    `auth_request` renames freely (`proxy_set_header <AppName> $upstream_http_variable_<id>`)
    → the app sees stable names decoupled from the engine (this is how that product's
    reference nginx config pins `Remote-User`/`Affiliation`/`Mail`). Traefik ForwardAuth
    **cannot rename** (`authResponseHeaders` copies by-name; no value-copying
    middleware in stock Traefik) → the app consumes the engine's names as-is, or a
    plugin/shim is added. So an app with a fixed header contract couples to the
    engine naming under ForwardAuth but not under the nginx-rename model — likely a
    reason that product chose the latter.
  - **Everything else stays engine-agnostic** (Jeff, 2026-07-09): no other part of
    the generated config is this implementation-dependent, so this is the one
    boundary to call out explicitly in the engine-adapter interface (§2/§3/§12).

---

## 10. The next step: a manual de-risking spike (delivered)

The operator is the well-understood, low-risk half; the **shibauthorizer-behind-
a-non-nginx-gateway flow is the load-bearing unknown.** Automating an unproven
flow is backwards — so the next step is a manual, operator-free, single-app,
single-host spike that proves the round-trip end to end, then the operator
becomes "generate this config."

The spike package (separate files in this bundle) tests four hypotheses:
1. A plain `fastcgi_pass` to the authorizer surfaces a usable HTTP response
   (200 + `Variable-*` on allow; 302 + `Location` on deny). Fallback if not:
   the `ngx_http_shibboleth` module.
2. The SP reconstructs the real request URL from the `X-Forwarded-*` → FastCGI
   param mapping (RequestMap applies to the real host/path, not `/authcheck`).
3. The exact attribute header names the authorizer emits, for Traefik
   `authResponseHeaders`.
4. The SP builds correct HTTPS ACS/handler URLs and a usable cookie behind a
   TLS-terminating gateway (`handlerSSL`/`cookieProps=https`).

It uses **samltest.id** as the IdP to sidestep InCommon registration latency,
**single-host** to isolate forward-auth risk from cross-host cookie risk, and
**single-replica** to skip the shared store for now.

---

## 11. Open gaps / decisions still pending

- **Cross-host centralized ACS (phase 2):** a dedicated auth host + parent-domain
  cookie + RelayState return, so metadata is stable and adding an app never
  re-touches the IdP. Strongly preferred at SaaS scale over per-vhost ACS
  (which churns metadata/registration), at the cost of parent-domain cookies.
  Validate after the single-host spike proves the core.
- **Container process model:** confirmed single-pod multi-process via supervisord
  for the spike; revisit multi-container (shibd + nginx sharing the socket
  volume) for production idiomaticity.
- **memcached clustering + replay cache:** the operationally fussy corner —
  load-test that sessions genuinely share across replicas before relying on HA.
- **shibd hot-reload scope:** which `shibboleth2.xml` changes reload in place vs
  require a roll (drives the rollout design).
- **Edge header hygiene (spike finding — SECURITY; END-TO-END VERIFIED through live
  Traefik ForwardAuth, 2026-07-09):** the gateway must strip client-supplied identity
  headers before the app sees them. Reproduced the whole matrix against a real Traefik
  v3.5 ForwardAuth → SP → whoami chain (session minted through Traefik via a real
  mocksaml assertion), injecting spoofed headers on the browser request:
  - **Unauthenticated + injected `Variable-*`** → fail-closed (302 relayed to the IdP,
    never reaches the app; the authorizer's `/authcheck` response leaks no `Variable-*`).
  - **Authenticated, header listed in `authResponseHeaders`** (`Variable-email`, `-uid`,
    `-REMOTE_USER`) → **overwritten** with the authoritative session value; the injected
    `attacker@evil.com`/`root` is discarded. The SP-owned guarantee is airtight: the
    authorizer derives its response purely from the decoded session, immune to request
    injection (verified — injected `Variable-email` came back `alice@example.com`).
  - **Authenticated, header NOT listed** (`Variable-Groups`, `Variable-Entitlement`,
    and the authorizer's own control headers `Variable-Shib-Session-ID`/`-Shib-Handler`/
    `-AUTH_TYPE`/… — the authorizer emits ~14 `Variable-*`, only the ~6 listed are
    handled) → **rides straight through to the app.**

  **Corrected mechanism** (supersedes the earlier "app-bound request never traverses a
  component we own" framing): a Traefik `headers` middleware chained *before* the
  ForwardAuth **does** run on the app-bound request and **can** clear a header by exact
  name (`customRequestHeaders: {Name: ""}` removes it — verified: a forged
  `Variable-Shib-Session-ID` and `Variable-Groups` were both neutralized when
  enumerated). The real limit is that **stock Traefik has no prefix glob** — it can only
  clear names you enumerate, so an unbounded `Variable-*` namespace (names from other
  federations/apps the operator can't know) always leaks. The **nginx `auth_request`**
  model closes the unbounded case: `more_clear_input_headers 'Variable-*'` (headers-more
  **does** support the `*` glob — confirmed in nginx-http-shibboleth's stock
  `shib_clear_headers`, which globs `'Shib-*'`) wildcard-clears the whole prefix on the
  app-bound request, and it renames to clean app-facing names so the app never sees
  `Variable-*` at all. So the distinction between the two edges is **prefix-glob
  capability, not request-path traversal**; it is still the **second** structural
  robustness edge of the nginx-rename model over ForwardAuth (first = free header
  rename, §9 addendum).

  **Spike-level decision — the operator's clear-list per model:**
  1. **Traefik ForwardAuth:** (a) list the full identity vocabulary in
     `authResponseHeaders` (overwrite = authoritative — already done); (b) emit an
     enumerate-clear `headers` middleware for the authorizer's *control* vocabulary
     (`Variable-Shib-*`, `Variable-AUTH_TYPE`, …) that isn't meant to reach the app;
     (c) since (b) can't cover unknown names, the security contract is **apps under
     ForwardAuth trust ONLY the listed `authResponseHeaders` set** — anything else
     `Variable-*` is attacker-controllable. To actually close the unbounded gap, either
     add a **wildcard-strip plugin** (Yaegi/WASM regex-removing `^Variable-` pre-gate)
     or ship the nginx edge for that app.
  2. **nginx `auth_request`:** emit `more_clear_input_headers 'Variable-*'` (glob) — the
     prefix is fully cleared on the app-bound request; no residual gap.

  GEP-1494 / Envoy ext-authz have their own allow/deny header semantics to re-verify.
- **Real IdP/federation registration:** has external lead time; start in parallel
  with the spike.
- **GEP-1494 302-relay spike (NEW, 2026-07-09):** before the operator commits to
  gateway-native ext-authz beyond Traefik, prove that Cilium's / Envoy Gateway's
  `ExternalAuth` HTTP mode relays the SP's 302-to-IdP to the browser (not just
  200/deny). Outcome decides gateway-native-everywhere vs. shipping the
  nginx-module edge for non-relaying dataplanes. See §9 addendum.
- **ForwardAuth target = endpoints, not ClusterIP:** the operator must emit the
  auth address against a headless Service (Traefik) or a `backendRef` (GEP-1494/
  Envoy) so it survives gateway→Service-CIDR egress restrictions. See §9 addendum
  / spike fix O.
- **Hub/Agents watch:** revisit when the remoting protocol stabilizes and a
  third-party (Rust) Agent against the Hub becomes viable.

---

## 12. One-line summary

Build a Go operator and two CRDs that wrap a containerized Shibboleth SP v3 as a
gateway-portable, forward-auth authenticator — owning the orchestration,
borrowing the SAML — with a swappable-engine boundary that lets Hub/Agents drop
in later. De-risk the one unknown (shibauthorizer behind Traefik forward-auth)
with a manual spike before writing the operator.
