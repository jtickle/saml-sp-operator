---
slug: saml-sp-operator
title: SAML SP operator — project thread
status: in_progress
created: 2026-07-02
updated: 2026-07-09
---

# Thread: SAML SP operator — project thread

The canonical, ongoing thread for the whole SAML SP operator effort. Work
continues here across chapters (spike → operator build → phase-2 cross-host
ACS → …) rather than spawning sibling threads.

## Goal (overall)

Build a Go operator + two CRDs (`SPInstance`, `AppIntegration`) that wrap a
containerized **Shibboleth SP v3** as a gateway-portable, forward-auth
authenticator — own the orchestration, borrow the SAML — with a swappable-engine
boundary so Hub/Agents can drop in later (`DESIGN.md` §12).

## Current chapter: the forward-auth de-risking spike (DESIGN §10) — ✅ PROVEN 2026-07-09

Prove the encapsulated `shibd + shibauthorizer + shibresponder + nginx`
assembly works behind **Traefik forward-auth**, end to end, against a real IdP,
by hand — before writing the operator. If a single app round-trips, the operator
is "just" generating this config from CRDs.

**DONE.** Full browser round-trip in-cluster on the :30443 NodePort path: Firefox →
mocksaml → session → ForwardAuth gate → whoami, with identity attributes flowing
(`Variable-Email/Firstname/Lastname/Uid/Remote_user/Shib-Identity-Provider`, jackson@example.com,
confirmed 2026-07-09). All four DESIGN §10 hypotheses green; the module is NOT needed
for Traefik ForwardAuth (Hyp #1). Next chapter: the **Go operator** (DESIGN §7). Small
note: Traefik canonicalizes header casing (`Variable-REMOTE_USER` → `Variable-Remote_user`).
Remaining verifications are operator-scope, not spike blockers (see Next Steps).

## Deployment target (this cluster)

- **Cluster:** the target cluster, ns `shib-spike`. Traefik Gateway API (`kubernetesGateway`
  + `kubernetesCRD` providers).
- **Real topology (Jeff, 2026-07-08):** browser → **load balancer on STANDARD
  80/443** (per matching domain) → k8s node **NodePort 30080/30443** → Traefik →
  SP. **Standard ports are the target; the operator should optimize for 443.**
  Hitting `https://…:30443/` on a node directly is a **TESTING CONVENIENCE**
  (bypasses the LB), NOT production — so the `:30443` pinning (fixes G/I/L, the
  explicit `port=` in fix N, `SHIBSP_SERVER_PORT=30443`) is a **test artifact**.
  On the 443 target those pins simplify away: `handlerURL` drops `:443`,
  `SHIBSP_SERVER_PORT=443`, and a bare `<Host name>` auto-covers `:443` (no fix-N
  port needed). Non-standard ports *should* still work (robustness — proven by the
  spike), but are not the target. **What's durable across the whole topology,
  port or not: `SHIBSP_SERVER_SCHEME=https`** — the SP always sits behind ≥1
  TLS-terminating hop (LB or Traefik) and sees plain HTTP, so without the override
  it builds `http://` self-URLs (fix M's scheme half). Jeff is **ignoring TLS
  certs** for the spike (gateway presents `origin-ca`, reads as self-signed on
  a direct node hit → click through). **Operator rule:** render
  `SHIBSP_SERVER_{NAME,SCHEME,PORT}` + `handlerURL` + the `<Host>` rule from the
  external URL; 443 is the simple common path, an odd port needs the port echoed
  in all three.
- **Gateway is platform-owned:** shared `traefik-gateway` in the **`traefik`**
  namespace (listeners `web`:8000 HTTP, `weblong`:8444 HTTPS, `websecure`:8443
  HTTPS, all `allowedRoutes.namespaces.from: All`, TLS `origin-ca`). The SP's
  HTTPRoutes attach cross-namespace to it; the spike does NOT own a Gateway.

## Where things stand (2026-07-03)

**Spike bring-up in-cluster — a long debugging chain, config-only, no image
rebuilds** (the image `shib-authenticator:spike` is unchanged; everything is
mounted from the `shib-config` ConfigMap = `shibboleth2.xml` + `nginx.conf`).
Original bench fixes A–E are in commit `e596bba`. The in-cluster fixes:

- **F — Gateway attachment** (`a951e12`): the spike had stood up its own Gateway
  in `shib-spike`; the cluster already owns `traefik-gateway`/`traefik`. Both
  HTTPRoutes now use cross-namespace `parentRefs` → `traefik-gateway`,
  `namespace: traefik`, `sectionName: websecure`. Cross-ns attach is allowed by
  the Gateway's `from: All`. Backends stay in-namespace → **no ReferenceGrant
  needed**. `k8s/20-gateway.yaml` reduced to a comment-only stub (mirrors the
  product boundary: operator owns routes + Middleware, NOT the Gateway).
- **G — NodePort port** (`5f48ca1`): browser authority is `:30443`. `nginx.conf`
  `SERVER_PORT 443 → 30443` in BOTH the responder (`/Shibboleth.sso`) and
  authorizer (`/authcheck`) blocks, so the ACS the SP advertises carries :30443.
- **H — Egress NetworkPolicy** (`157afe9`): the cluster enforces a **global
  default-deny** NetworkPolicy. shibd couldn't reach `mocksaml.com` → the
  remote-URL MetadataProvider failed init (`unable to connect socket`) → the
  Application came up degraded → every `/Shibboleth.sso` handler 500'd. Added
  `k8s/05-networkpolicy.yaml`: `allow-all-egress`, `allow-intra-namespace-ingress`,
  and `allow-traefik-ingress` (ingress from the `traefik` ns so the gateway can
  reach the pods — else the browser flow 502s). All spike-only permissive.
  (A local-file-metadata workaround was tried first, then **reverted** once the
  netpol was the real fix.)
- **I — Fully-qualified handlerURL** (`105369f`): with the non-standard port, a
  RELATIVE `handlerURL` lets the SP normalize the https port away, so the handler
  base (`https://host/Shibboleth.sso`) doesn't prefix-match the real request
  (`https://host:30443/…`) → shibresponder rejects EVERY handler with **"should
  only be used for Shibboleth protocol requests."** Pinned
  `handlerURL="https://SP_HOST_PLACEHOLDER:30443/Shibboleth.sso"`.
- **J — Readiness decoupled from the SP** (`105369f`): the old probe hit
  `/Shibboleth.sso/Metadata`, which can't be satisfied by a kube-probe (it sends
  the pod IP, not the external host:port the pinned handlerURL requires). Added a
  dumb `location = /healthz { return 200 }` in nginx and pointed the probe there.
  **CAVEAT:** too dumb — it reports Ready even when shibd is FATAL. During this
  spike, always glance at the log tail after a config change; the operator needs
  a real SP health check.
- **K — Illegal `--` in an XML comment** (`e077752`): my handlerURL comment
  contained `--`, which is illegal inside `<!-- -->`. shibboleth2.xml failed to
  parse → shibd/authorizer/responder all exited (status 254/1) → FATAL → nginx
  502. A regression of the original **fix A**. Now validate XML locally before
  pushing.
- **L — HTTP_HOST must carry the port** (`c574b14`, **AWAITING VERIFICATION**):
  even with I in place, the responder still rejected all handlers. Cause: the SP
  reconstructs its self-URL **authority from `HTTP_HOST`**, and when `HTTP_HOST`
  has no port it defaults to the SCHEME port (443), **not** `SERVER_PORT`. nginx
  does NOT auto-forward the client `Host` header to FastCGI, and `$host` strips
  the port — so `HTTP_HOST` was portless → self-URL `https://host/…` ≠ the
  `:30443` handler base → reject. Fix: `fastcgi_param HTTP_HOST $host:30443` in
  the responder block. (Confirmed by the nginx-shib CONFIG.rst troubleshooting
  section: the FastCGI env must match handlerURL's protocol/port/path exactly,
  especially with an absolute URL + custom port; and FastCGI apps don't log
  their own rejections, only shibd does.)

- **M — THE REAL FIX: `SHIBSP_SERVER_*` process env, not fastcgi_params**
  (2026-07-08, root-caused locally against `shib-authenticator:spike` via Docker).
  Fix L was necessary-but-INSUFFICIENT; it aimed at the wrong knob. With L live
  in-cluster the responder STILL 500'd. Reproduced identically in a local
  container and turned on the FastCGI responder's DEBUG log (`native.logger`,
  which the FastCGI apps use — but its stock appender is `LocalSyslogAppender`
  → syslog/void, and a `ConsoleAppender` corrupts the FastCGI stdout channel →
  use a `RollingFileAppender` to a **world-writable** path; the responder runs as
  a non-`_shibd` user and can't write `/var/log/shibboleth`). The DEBUG line
  `Shibboleth.FastCGI: mapped http://host:30443/Shibboleth.sso/Login … to default`
  was the smoking gun: host+port were RIGHT (`:30443`), the **scheme was `http`**,
  so it failed the prefix-match against the `https://` handlerURL. `grep`-ing the
  `shibresponder` binary showed it references `SHIBSP_SERVER_NAME` /
  `SHIBSP_SERVER_SCHEME` / `SHIBSP_SERVER_PORT` and **does NOT reference the
  `HTTPS` or `REQUEST_SCHEME` env-var names at all** — it derives the scheme from
  the port unless `SHIBSP_SERVER_SCHEME` is set, and `30443` is not a recognized
  HTTPS port → `http`. Setting the three as **process environment** (via
  supervisord `environment=`, then confirmed via plain container-level `-e` since
  supervisord PID 1 propagates its env to the children) flipped the reconstruction
  to `https://host:30443/…` → `/Login` **302 to mocksaml**, `/Metadata` **200**.
  Proven with the PRISTINE deployed `nginx.conf` (zero config edits) → the fix is
  **purely additive**: a Deployment `env:` block. Committed to `k8s/10-authenticator.yaml`
  (env block, `SP_HOST_PLACEHOLDER` for the name) + README sed line.
  **Consequence:** fix L's `HTTP_HOST $host:30443` and the responder block's
  `SERVER_PORT`/`SERVER_NAME`/`HTTPS` fastcgi_params are now **redundant** for the
  responder self-URL (the `SHIBSP_*` env is authoritative) — a follow-up
  simplification, not stripped yet.

- **N — RequestMap `<Host>` needs explicit `scheme`+`port` on a non-standard port,
  or the authorizer FAILS OPEN** (2026-07-08, caught pre-flighting the authorizer
  locally before the browser flow). A bare `<Host name="host">` auto-expands to
  STANDARD ports only (`http://host`, `:80`, `https://host`, `:443`) — it never
  generates a key for `https://host:30443`. The authorizer reconstructs
  `https://host:30443/` (scheme now correct via fix M), matches NO RequestMap key,
  so `requireSession` is never applied and shibauthorizer returns **200 for
  UNAUTHENTICATED traffic** → Traefik ForwardAuth waves everyone through. `/Login`
  hid this because handlers aren't subject to `requireSession`; the *access
  decision* is. Fix: `<Host name="…" scheme="https" port="30443" …>` (schema
  `HostType` supports both). Verified locally: `/authcheck` no-session **200 →
  302** after the change. On plain 443 this simplifies away. **Operator MUST emit
  scheme+port on the RequestMap rule from each app's external URL** — this is the
  single most important security lesson of the spike (the whole ForwardAuth gate
  fails open otherwise).
  - **Debugging trap hit here:** `sed -i` on a Docker **bind-mounted file**
    replaces the inode, so the container keeps seeing the OLD file — every
    RequestMap edit silently no-op'd until the container was recreated. Edit then
    recreate (or mount the dir), never `sed -i` a bind-mounted file.
  - **Regression:** the fix-N comment reintroduced an illegal `--` inside
    `<!-- -->` (fix K again) → shibd parse FATAL → nginx 500 (empty body). Fixed
    (`1a13c3b`) and this time validated by loading the sed'd file into REAL shibd
    (all procs RUNNING), not just eyeballing. Lesson restated: validate the
    committed XML in shibd before pushing, not the pre-edit file.

- **O — Traefik ForwardAuth must target a HEADLESS Service, not the ClusterIP**
  (2026-07-09, from Traefik gateway logs). With fixes M+N deployed and shibd
  healthy, the protected app route still 500'd with EMPTY nginx logs — the request
  never reached nginx. Traefik's log was explicit: `Error calling
  http://shib-authenticator…:8080/authcheck … dial tcp 10.0.0.1:8080: connect:
  network is unreachable`. The ForwardAuth Middleware's HTTP client dials the
  **Service ClusterIP**, which is unreachable from the gateway. **Mechanism
  corrected 2026-07-09 (live Calico policy):** NOT an egress-policy block — the
  `GlobalNetworkPolicy` exempts the `traefik` ns from the default-deny, so gateway
  egress is open; `ENETUNREACH` to the ClusterIP with egress open = a kube-proxy /
  Service-VIP resolution (routing) matter, not policy. Lesson stands and is
  stronger: gateway→ClusterIP reachability is CNI/kube-proxy-specific and fragile.
  The `/Shibboleth.sso` route worked throughout because Gateway-API routing sends
  Traefik to the **pod endpoint IP**, dodging the ClusterIP entirely. Fix: added
  `shib-authenticator-headless` (`clusterIP: None`) and pointed the Middleware at
  it → the FQDN resolves to the pod IP (the proven-reachable path), no change to
  the platform-owned `traefik` namespace. `allow-traefik-ingress` (fix H) was
  never the problem — it correctly permits the pod ingress. **Operator
  implication:** emit the ForwardAuth address against a headless Service so the SP
  is self-sufficient under gateway-egress restrictions. (Alternative Jeff could
  take instead: a platform egress NetworkPolicy allowing `traefik` → the SP
  Service CIDR; keeps ClusterIP but needs platform-side access.) **AWAITING
  IN-CLUSTER VERIFICATION.**

- **Research: ext-authz best practices** (2026-07-09, captured in **DESIGN §9
  addendum + §11**). Web research on how similar solutions are built. Key steers:
  **(1)** the **GEP-1494 `ExternalAuth`** HTTPRoute filter is the portable,
  Traefik-agnostic standard, its **HTTP mode maps 1:1 to our SP edge** (200+headers
  / non-200), and **Cilium + Envoy Gateway already ship the filter** — so design
  the operator's attachment layer as swappable (Traefik `Middleware` now, GEP-1494
  `ExternalAuth` as it matures); the SP image is unchanged across all of them.
  **(2)** The **302-relay is the load-bearing portability unknown** — Traefik
  relays it (interactive SAML works), but GEP-1494 HTTP mode is spec-silent on it;
  must spike per dataplane, else fall back to the nginx-module edge (that product's model).
  **(3)** Standards use **`backendRef` → endpoints**, which structurally avoids the
  fix-O ClusterIP-egress problem — confirming endpoint-based access is the norm,
  and Shibboleth-for-SAML is correct (lighter oauth2-proxy/Keycloak alternatives
  are OIDC, not SAML).

**What's proven now:** shibd inits clean, MetadataProvider loads mocksaml over
open egress, config parses, all procs RUNNING, pod Ready. **Responder (fix M):**
`/Shibboleth.sso/Login` → 302 to the IdP, `/Metadata` → 200 (verified IN-CLUSTER
2026-07-08 on the real gateway :30443). **Authorizer (fix M + N, local repro):**
`/authcheck` with no session → 302 to SSO (fails open → fixed by N).
- **O verified + BROWSER ROUND-TRIP PROVEN IN-CLUSTER** (2026-07-09). After
  headless fix O, the protected `/` route stopped 500ing; Firefox →
  `https://sp…:30443/` → 302 → mocksaml login → assertion → **session minted**
  (`_shibsession_…` cookie) → **whoami reached** (200 through the ForwardAuth gate,
  not a 302). DESIGN §10's core hypothesis — encapsulated SP behind a non-nginx
  gateway, full SAML round-trip — is proven end to end in-cluster on the :30443
  NodePort path.
- **Hyp #3 (attribute export) — SOLVED + verified locally** (2026-07-09, commit
  `7fc438f`). Drove a real mocksaml assertion end to end against the spike image
  and captured ground truth. mocksaml releases exactly 4 attributes —
  `id`/`email`/`firstName`/`lastName`, `NameFormat=unspecified`, plain (non-URI)
  Names — which the STOCK `attribute-map.xml` drops → `/Session` showed no
  attributes and `REMOTE_USER` was empty. Fixes: **(a)** new `attribute-map.xml`
  (mounted over stock) mapping the 4 → ids `email`/`firstName`/`lastName`/`uid`;
  **(b)** `REMOTE_USER="email uid"` (mocksaml has no eppn/uid); **(c)** the
  authorizer exports REMOTE_USER + each attr as **`Variable-<id>`** HTTP response
  headers (NOT `Remote-User`/`Variable-mail`) — corrected the Middleware
  `authResponseHeaders` to the real names. Verified: `/Session` lists all four;
  `/authcheck` → **200** with `Variable-REMOTE_USER: alice@example.com`,
  `Variable-email/firstName/lastName/uid`, `Variable-Shib-Identity-Provider`.
  The stock `attribute-policy.xml` passed mocksaml's flat attrs (no scoping), so
  no policy change here — a real InCommon IdP with scoped eppn still needs it.
  **How I drove it without a browser:** scripted the SP-initiated flow with curl
  (mocksaml `/api/saml/auth`, email must end `@example.com`), decoded the signed
  SAMLResponse, POSTed it to the ACS. Gotcha: the `_shibsession` cookie is
  `secure`, so over plain HTTP curl withholds it — send it via an explicit
  `Cookie:` header to exercise `/Session` + `/authcheck` locally.
  **Still to confirm in-cluster:** redeploy (ConfigMap now includes
  `attribute-map.xml`) + Firefox → whoami shows the `Variable-*` headers; and the
  spoof/header-hygiene check (client-injected `Variable-*` must not survive — the
  operator strips them at the edge; DESIGN §11).

## Next Steps (resume here)

**Status: all four spike hypotheses proven + the header-hygiene security check is
now VERIFIED end-to-end.** M (scheme), N (authorizer fail-open), O (headless
ForwardAuth), and the full browser round-trip are green IN-CLUSTER; Hyp #3
(attributes) confirmed in-cluster; and the edge header-hygiene spoof matrix is
reproduced against live Traefik with the spike-level mitigation decided (DESIGN §11).
**The spike is functionally complete — the remaining item is the Go operator.**

1. ✅ **DONE — Hyp #3 confirmed in-cluster** (2026-07-09): whoami received
   `Variable-Email/Firstname/Lastname/Uid/Remote_user/Shib-Identity-Provider`
   (jackson@example.com). The spike is complete.
2. ✅ **DONE — Spoof / header-hygiene check VERIFIED end-to-end** (2026-07-09).
   Reproduced the full matrix against a live Traefik v3.5 ForwardAuth → SP → whoami
   chain locally (session minted *through* Traefik via a real mocksaml assertion, so it
   mirrors the browser path). Results:
   - **Unauth + injected `Variable-*`** → fail-closed (302 relayed; authorizer leaks no
     `Variable-*`). Also re-confirmed the Hyp #1 302-relay through real Traefik.
   - **Authed, LISTED header** (`Variable-email/uid/REMOTE_USER`) → **overwritten** with
     the authoritative session value; injected `attacker@evil.com`/`root` discarded. The
     SP-owned guarantee is airtight — the authorizer's response derives purely from the
     decoded session, immune to request-header injection (proven directly against
     `shib-repro`: injected `Variable-email` returned `alice@example.com`).
   - **Authed, UNLISTED header** (`Variable-Groups`, `Variable-Entitlement`, and the
     authorizer's own control headers `Variable-Shib-Session-ID/-Shib-Handler/-AUTH_TYPE`
     — it emits ~14 `Variable-*`, only ~6 are listed) → **rides straight through to the
     app.** This is the gap.
   - **Mechanism correction (important):** a Traefik `headers` middleware chained before
     ForwardAuth **does** act on the app-bound request and **can** clear a header by
     *exact name* (`customRequestHeaders: {Name: ""}` — verified it neutralized a forged
     `Variable-Shib-Session-ID`). The real limit is **no prefix glob** in stock Traefik,
     so an unbounded `Variable-*` namespace always leaks. The nginx model's
     `more_clear_input_headers 'Variable-*'` **glob** (headers-more supports `*`; the
     stock `shib_clear_headers` globs `'Shib-*'` — confirmed from source) wildcard-clears
     the whole prefix. So the nginx-vs-ForwardAuth edge is **prefix-glob capability, not
     request-path traversal** — corrects the earlier §11 framing.
   - **Decision (spike-level mitigation), written to DESIGN §11:** under Traefik —
     list identity vocab in `authResponseHeaders` (overwrite = authoritative) + enumerate-
     clear the authorizer *control* vocab + contract that **apps trust ONLY the listed
     set**; close the unbounded gap with a wildcard-strip plugin or the nginx edge. Under
     nginx — `more_clear_input_headers 'Variable-*'` fully closes it. Operator emits the
     clear-list per model.
   - **Harness note:** driving the authenticated path locally requires minting the session
     *through* Traefik (the `/Shibboleth.sso` ACS routed through it too). A session minted
     by hitting `:8080` directly and then replayed through Traefik is **removed** by the SP
     on first use (even with `checkAddress="false"`) — an artifact of the off-proxy mint
     path, not a spike bug (in-cluster the browser mints through the gateway, which works).
     Drivers: `scratchpad/mint.py` (direct) + `mint_tf.py` (through-Traefik).
3. 🚧 **IN PROGRESS — Go operator scaffolded** (2026-07-09, branch
   `gsd/operator-scaffold`, commit `4a51f9c`). kubebuilder go/v4 project at the repo
   root; two namespaced CRDs under **`saml.tickletechnologies.com/v1alpha1`** each with a
   controller stub. First-cut `v1alpha1` types written from DESIGN §5 (SPInstance:
   entityID, keypair Secret ref, IdP/federation trust, `allowedNamespaces` consent,
   session-store ref, status = metadataURL/boundCount/configHash; AppIntegration:
   cross-ns spInstanceRef, namespace-local targetRef→HTTPRoute, attribute→header map,
   session/authz policy, SLO opt-out). Reconcile logic NOT written yet (empty
   reconcilers); `make build` + envtest green. Spike artifacts relocated to `spike/`
   so kubebuilder owns the root; spike image CI repointed at `spike/`.
   **Decisions this session:** API group `saml.tickletechnologies.com` (domain
   `tickletechnologies.com` + group `saml`; the Tickle Technologies *company* domain,
   chosen over `jefftickle.com` so the CRD identity is the company product's, not
   personal — best practice is a group you own, cert-manager.io pattern); process =
   scaffold now, GSD-phase the reconcile logic; branch base =
   land spike→main then fork (operator branch based at the spike tip = identical to a
   FF'd main). Go toolchain (1.26.5) + kubebuilder v4.15.0 installed under
   `~/go-sdk` / `~/go/bin` (PATH persisted in `~/.bashrc`).
   **⏳ ACTION FOR JEFF (main-affecting, PR-only per hygiene; no `gh` in-session):**
   land `spike` → `main` via a browser PR, merged as **fast-forward or merge-commit,
   NOT squash** — a squash would rewrite the base and make `gsd/operator-scaffold`'s
   history diverge. Then `gsd/operator-scaffold` PRs into `main`.
   **Next on the operator:** ✅ DONE (2026-07-10) — project GSD-initialized via
   `/gsd-new-project`. `.planning/` now holds PROJECT.md, config.json (adapted from
   a separate product), 4-agent research + SUMMARY.md, REQUIREMENTS.md (31 v1 reqs), and
   a **6-phase ROADMAP.md** (commit `065ed7a`): (1) pure render/aggregation package →
   (2) SPInstance static path + production foundations → (3) AppIntegration resolution-
   only → (4) cross-ns aggregation → (5) Middleware+Conflict+finalizer (end-to-end) →
   (6) hardening/observability closeout. Adopts the architecture research's dependency-
   ordered sequence; every phase verifiable against the live spike image. **Ready for
   `/gsd-plan-phase 1`.** Deferred spike cleanups (redundant fastcgi_params; real SP
   readiness probe) folded into RENDER-*/SPI-03. Config note: took that product's
   `branching_strategy: none` + `use_worktrees: true` — flag if phase-branches wanted.
4. **Follow-up spike chapter:** GEP-1494 `ExternalAuth` 302-relay on Cilium/Envoy
   Gateway (DESIGN §9 addendum / §11) — decides gateway-native-everywhere vs the
   nginx-module edge for non-relaying dataplanes.

## Learnings to carry into the operator

- **External URL (scheme/host/port) is a first-class config input — delivered as
  `SHIBSP_SERVER_NAME`/`SHIBSP_SERVER_SCHEME`/`SHIBSP_SERVER_PORT` process env on
  the SP container** (read by the FastCGI apps via `getenv`, propagated by
  supervisord PID 1; a Deployment `env:` block suffices — no image rebuild). This
  is the AUTHORITATIVE way to set the SP's self-URL behind a TLS-terminating proxy
  on a non-standard port; per-request `HTTPS`/`SERVER_PORT`/`HTTP_HOST`/`REQUEST_SCHEME`
  fastcgi_params do NOT drive scheme reconstruction (the responder derives scheme
  from the port and only `SHIBSP_SERVER_SCHEME` overrides it). Still also pin an
  absolute `handlerURL` with the port so the advertised ACS carries `:30443`. The
  operator generates all three env vars + handlerURL from each SP's external URL.
  See fix M. (Superseded the earlier belief that `HTTP_HOST`/`SERVER_PORT`
  fastcgi_params were the fix — they were necessary-looking but inert.)
- **Debugging the FastCGI apps:** they log via `native.logger` (not `shibd.logger`);
  its stock appender is syslog (void in a container) and a `ConsoleAppender`
  corrupts the FastCGI stdout protocol. Use a `RollingFileAppender` to a
  world-writable path (the apps run as a non-`_shibd` user). The DEBUG line
  `Shibboleth.FastCGI: mapped <URL> to <appId>` shows the exact reconstructed URL.
- **LOCKED — exported header naming is engine-determined (the swappable-engine
  leak).** SP3's FastCGI `shibauthorizer` hard-codes the `Variable-` prefix
  (confirmed: literal in the authorizer binary, no `shibboleth2.xml` knob). Operator
  controls the suffix (`attribute-map` id) + which attrs, NOT the prefix/shape.
  It's the ONLY engine-implementation-dependent part of the identity contract
  (Jeff) — so swapping the engine (SP3→SP4→other) is a header-contract-affecting
  change; SP4 likely drops this. The rename layer is the shock absorber: nginx
  `auth_request` renames freely (that product's model → stable app-facing names); Traefik
  ForwardAuth can't rename → app consumes `Variable-*` as-is. Detailed in DESIGN §2
  + §9 addendum.
- **SECURITY — header hygiene, second nginx-vs-Traefik edge** (spike-verified
  2026-07-09). Unauthenticated + injected `Variable-*` = fail-closed (302, no
  session). But an *authenticated* request with a `Variable-*` NOT in
  `authResponseHeaders` (e.g. `Variable-Groups`) rides through — Traefik overwrites
  only listed names and can't wildcard-strip a prefix. Under ForwardAuth the
  app-bound request never traverses a component we own, so hygiene is
  enumerate-clear + app-must-trust-only-the-listed-set (or a plugin). The nginx
  `auth_request` model wildcard-clears (`shib_clear_headers`/`more_clear_input_headers`)
  because the app-bound request goes through nginx. Same root cause as the rename
  difference. Operator generates the clear-list per model. DESIGN §11.
- **Egress-restricted clusters break remote metadata.** Real federation
  (InCommon) metadata can't be fetched from a URL without a cluster egress
  allowlist or an operator-managed in-cluster metadata mirror/refresh. Major
  design input, not a spike detail.
- **The operator does NOT own the Gateway.** It attaches routes + the ForwardAuth
  Middleware to a platform-provided Gateway (cross-namespace parentRef, sectionName
  = the HTTPS listener). NetworkPolicy: the target ns needs egress + ingress from
  the gateway's namespace.
- **Readiness needs a real SP check**, not a dumb nginx 200 — one that sends the
  correct external host:port so it actually exercises a handler.
- **FastCGI apps log almost nothing**; shibd.log carries init + RequestMapper
  mappings. Rejections surface only as the responder's stderr + the 500 body.

## Follow-ups / deferred (not blocking the spike)

- **nginx.conf simplification (post fix M):** the responder block's `HTTPS on` /
  `SERVER_PORT 30443` / `SERVER_NAME $host` / `HTTP_HOST $host:30443` are now
  redundant (the `SHIBSP_SERVER_*` env is authoritative for the self-URL). Verify
  the authcheck block's `SERVER_PORT`/`HTTP_HOST` likewise, then strip the dead
  params so the operator emits the minimal nginx.conf. Not blocking; do after the
  authorizer + browser flow are green so we change one thing at a time.
- **redirectLimit**: shibd warns it operates as an open redirector; set it in
  operator-generated config.
- **Node 20 / docker actions** deprecation in CI: benign; bump all four together
  when node24 releases exist for the `docker/*` actions.
- **arm64**: CI publishes linux/amd64 only.
- **Public planning + repo hygiene** (2026-07-09): the `origin` repo
  (`jtickle/saml-sp-operator`) is **PUBLIC**, and `.planning/` + `DESIGN.md` are
  committed to it. Scrubbed employer-infrastructure identifiers (cluster/node
  hostnames, an internal network-policy name, the origin-CA secret name, an
  internal ClusterIP, a vendor/cost backstory) from the working tree **and full
  git history** via `git filter-repo` (`--replace-text` + `--replace-message`),
  then force-pushed `spike` (`887650d`→`9623a0e`). Remote history verified clean.
  **Standing rule:** every commit here is public — keep employer/infra identifiers
  out of tree, planning, and commit messages. **Residual caveat:** the pre-scrub
  commits are now dangling on GitHub and may remain reachable by direct SHA (and
  in any forks/PRs/caches) until GitHub GC; ask GitHub Support to expedite if that
  matters.

## References

- `DESIGN.md` — full decision record (own the orchestration, borrow the SAML).
- `README.md` — spike runbook (mocksaml + prebuilt image).
- Image: `ghcr.io/jtickle/saml-sp-operator/shib-authenticator:spike`.
- IdP: mocksaml.com (BoxyHQ). entityID `https://saml.example.com/entityid`,
  metadata `https://mocksaml.com/api/saml/metadata`.
- Authoritative nginx+SP FastCGI config (the responder/"should only be used"
  troubleshooting): `github.com/nginx-shib/nginx-http-shibboleth` → `CONFIG.rst`.
- Shibboleth SP reverse-proxy / non-standard-port handlerURL guidance:
  Shibboleth wiki `SPReverseProxy`.
- Commits on `spike`: `e596bba` (bench A–E), `a951e12` (F gateway attach),
  `5f48ca1` (G port), `157afe9` (H netpol), `105369f` (I handlerURL + J healthz),
  `e077752` (K XML comment), `c574b14` (L HTTP_HOST port — awaiting verify).
