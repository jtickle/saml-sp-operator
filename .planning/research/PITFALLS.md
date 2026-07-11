# Pitfalls Research

**Domain:** Kubernetes operator (controller-runtime/Go) generating security-sensitive config — a Shibboleth SAML SP + gateway forward-auth wiring
**Researched:** 2026-07-09
**Confidence:** MEDIUM overall — HIGH for the six spike-proven items (first-party, verified in this repo's own de-risking spike); MEDIUM for general controller-runtime pitfalls (well-established community sources: the Kubebuilder Book, Ahmet Alp Balkan's `controller-pitfalls`, controller-runtime GitHub issues — cross-checked across multiple independent sources but not project-specific); MEDIUM-HIGH for the SSRF item (backed by a real, named CVE/GHSA advisory in a comparable SAML-metadata-fetch codepath).

---

## Critical Pitfalls

### Pitfall 1 [MUST-CARRY-FORWARD — spike-proven]: RequestMap `<Host>` without explicit scheme+port fails OPEN

**What goes wrong:**
A bare `<Host name="host">` RequestMap rule auto-expands to standard ports only (`http://host:80`, `https://host:443`). On any non-standard port, the authorizer reconstructs a self-URL like `https://host:30443/`, matches **no** RequestMap key, `requireSession` is never applied, and `shibauthorizer` returns **200 for unauthenticated traffic** — Traefik ForwardAuth waves everyone through. This is a silent, fully-open auth bypass with no error.

**Why it happens:**
The RequestMapper's implicit standard-port expansion is a convenience feature that quietly stops applying the moment the SP sits behind a non-standard port (any NodePort/test setup, and some multi-tenant SaaS port schemes). Handlers (`/Login`) hide the bug because they aren't subject to `requireSession` — only the access decision is, so a smoke test that only checks `/Login` redirects looks fine while the protected route is wide open.

**How to avoid:**
The operator must **always** emit explicit `scheme=` and `port=` attributes on every `<Host>` element, derived from each `AppIntegration`'s resolved external URL — never rely on the bare-hostname auto-expansion, even on port 443 (it happens to work there, but the renderer should not special-case "assume 443 is safe" logic that could regress).

**Warning signs:**
Authenticated smoke tests that only exercise `/Login` or `/Shibboleth.sso/*` handlers pass while the actual protected path is open; `/authcheck` returns 200 with no `Set-Cookie`/session for a completely fresh, unauthenticated client.

**Phase to address:**
Config-rendering phase (RequestMap templating) — with a mandatory negative test: unauthenticated hit to a protected path on a non-standard port must return non-200/302, never 200.

---

### Pitfall 2 [MUST-CARRY-FORWARD — spike-proven]: `SHIBSP_SERVER_*` process env — not `fastcgi_params` — drives SP self-URL scheme

**What goes wrong:**
The FastCGI responder/authorizer derive their self-URL scheme from `SHIBSP_SERVER_SCHEME`/`_NAME`/`_PORT` **process environment**, read via `getenv`. Per-request `fastcgi_param`s (`HTTPS`, `SERVER_PORT`, `HTTP_HOST`, `REQUEST_SCHEME`) look like the obvious knob and are NOT referenced by the responder binary at all — setting them has zero effect. Without the process-env override, the binary derives scheme from the port number and defaults to `http://` behind any TLS-terminating proxy, so every handler and the RequestMap match fail.

**Why it happens:**
This is invisible from the config file — nothing in `shibboleth2.xml` or `nginx.conf` documents that the *container's environment*, not its FastCGI parameters, is authoritative. Anyone porting nginx-based Shibboleth guidance (which is universally about `fastcgi_param`) will apply the wrong fix and get a plausible-but-wrong result.

**How to avoid:**
The operator must render `SHIBSP_SERVER_NAME`/`SHIBSP_SERVER_SCHEME`/`SHIBSP_SERVER_PORT` as Deployment `env:` (process-level), computed from each SP's external URL, alongside a fully-qualified `handlerURL` carrying the same port.

**Warning signs:**
`/Metadata` or `/Login` handlers reject with "should only be used for Shibboleth protocol requests"; FastCGI DEBUG log shows `mapped http://host:port/... to default` (scheme `http` when it should be `https`).

**Phase to address:**
Config-rendering / Deployment-rendering phase — bake as a rendering unit test asserting the three env vars are always present and consistent with `handlerURL`.

---

### Pitfall 3 [MUST-CARRY-FORWARD — spike-proven]: ForwardAuth must target a headless Service, not the ClusterIP

**What goes wrong:**
Traefik's ForwardAuth dials a URL string that resolves to the Service **ClusterIP**. From the gateway pod, that dial can fail (`network is unreachable`) even with egress fully open — this is a kube-proxy/Service-VIP resolution (routing-layer/CNI) issue, not a NetworkPolicy block, and it is CNI-specific (observed on Calico; unverified but plausible on Cilium too). Meanwhile normal Gateway API routing works fine because it resolves to pod **endpoints**, sidestepping the ClusterIP entirely.

**Why it happens:**
ForwardAuth's HTTP-client-dials-a-URL model doesn't get the same endpoint-aware routing that Gateway API `backendRef`/HTTPRoute resolution gets. This is easy to miss because the primary data-plane route (the app's own HTTPRoute) works throughout debugging, masking that the *auth* side-channel uses a different resolution path.

**How to avoid:**
The operator must always emit the ForwardAuth address against a **headless Service** (`clusterIP: None`) for the authenticator, so the FQDN resolves directly to endpoint IPs. When GEP-1494 `ExternalAuth`/Envoy `SecurityPolicy` land, their `backendRef` already resolves to endpoints natively — so the headless-Service requirement is specific to the Traefik-`ForwardAuth`-by-URL attachment model and should be scoped to that adapter, not treated as a universal rule.

**Warning signs:**
The app's own HTTPRoute serves fine, but every route behind the Middleware 500s with an **empty nginx access log** (request never arrived) and the Traefik/gateway log shows a dial error to the Service's ClusterIP.

**Phase to address:**
Gateway-attachment phase (Middleware/ForwardAuth rendering) — assert in an e2e test that the emitted ForwardAuth address is the headless Service FQDN, never the ClusterIP-backed one.

---

### Pitfall 4 [MUST-CARRY-FORWARD — spike-proven]: Edge header hygiene — unlisted `Variable-*` rides through under Traefik ForwardAuth

**What goes wrong:**
The authorizer emits ~14 `Variable-*` response headers on a successful auth check; Traefik's `authResponseHeaders` copies only the **listed** subset upstream, overwriting any client-injected value with the authoritative session value (this part is airtight — verified against real spoofing attempts). But any `Variable-*` header **not** in that list (attribute headers the operator didn't map, plus the authorizer's own control headers like `Variable-Shib-Session-ID`, `-Shib-Handler`, `-AUTH_TYPE`) rides straight through to the app unmodified — including a **client-injected** value, since stock Traefik has no prefix-glob header-clearing capability, only exact-name clearing.

**Why it happens:**
This isn't a request-path-traversal gap (an earlier framing that was corrected) — it's that stock Traefik can only enumerate-clear header names it's told about, and the authorizer's `Variable-*` namespace is unbounded (grows with every new attribute a federation IdP might release). nginx's `auth_request` model closes this because `more_clear_input_headers 'Variable-*'` supports a true wildcard.

**How to avoid:**
Under Traefik ForwardAuth: (a) list the full identity vocabulary in `authResponseHeaders` so those are always overwritten; (b) emit an enumerate-clear `headers` Middleware for the known authorizer control-vocabulary; (c) treat the residual gap as a hard security **contract**: apps behind a Traefik-ForwardAuth-fronted `AppIntegration` must be documented as trusting **only** the explicitly-listed `authResponseHeaders` set — any other `Variable-*` is attacker-controllable and must never be used for authz/identity decisions by app code. Close it fully with a wildcard-strip plugin (Yaegi/WASM regex `^Variable-`) or route that app through the nginx-`auth_request` edge instead.

**Warning signs:**
An app trusts a header not present in the operator's rendered `authResponseHeaders` list; a security review of the Middleware finds `authResponseHeaders` shorter than the authorizer's actual emitted set.

**Phase to address:**
Gateway-attachment phase for the enumerate-clear Middleware; documented explicitly in the `AppIntegration` CRD's user-facing contract/docs (not just code) so app teams don't assume "any `Variable-*` header is safe."

---

### Pitfall 5 [MUST-CARRY-FORWARD — spike-proven]: Dumb nginx readiness reports Ready while shibd is FATAL

**What goes wrong:**
A readiness probe that just hits a static `location = /healthz { return 200 }` (or bare TCP) reports the pod Ready even when `shibd` has crashed/FATAL'd and every real handler is 500ing. Kubernetes routes traffic to a pod that cannot actually authenticate anyone.

**Why it happens:**
A probe that actually exercises a Shibboleth handler is awkward to get right in a kube-probe context (the probe sends the pod IP as the request authority, not the external host:port the pinned `handlerURL` requires, so a naive "hit `/Shibboleth.sso/Metadata`" probe fails even when the SP is healthy) — so it's tempting to fall back to a dumb static 200, which then never fails.

**How to avoid:**
Build a real health check that proves `shibd` loaded (e.g., a lightweight local process/socket check the operator controls, or a handler probe that supplies the correct `Host` header matching the pinned external authority) rather than a bare TCP/static-200 endpoint. Never ship the dumb probe past the spike stage.

**Warning signs:**
Pod is `Ready` in `kubectl get pods` but every real request 500s and the log tail shows `shibd` exited/FATAL.

**Phase to address:**
Rollout & status phase — this is exactly the "real SP readiness probe" requirement already in `PROJECT.md`'s Active requirements; verify with a chaos test that kills `shibd` in a running pod and confirms readiness flips to NotReady.

---

### Pitfall 6 [MUST-CARRY-FORWARD — spike-proven]: Illegal `--` in XML comments FATALs shibd

**What goes wrong:**
`<!-- ... -- ... -->` is illegal per the XML spec. A single `--` inside a rendered comment (even from something as innocuous as an em-dash-adjacent double-hyphen or a generated timestamp range) causes `shibboleth2.xml` to fail to parse; `shibd`, the authorizer, and the responder all exit (FATAL), and nginx 502s across the board. This regressed **twice** in the spike from two independently-introduced comments.

**Why it happens:**
Comments feel like "safe," non-functional content, so they get the least scrutiny — exactly where a stray `--` sneaks in (from a generated changelog note, an operator-injected diagnostic comment, etc.).

**How to avoid:**
Never hand-author freeform comments into rendered config; if the operator emits any XML comments (e.g., "rendered by saml-sp-operator, do not edit"), sanitize/reject any content containing `--`, or avoid comments entirely in generated output. Validate the fully-rendered XML (parse it, ideally via `shibd -check` in a sidecar/init step or an equivalent XML-well-formedness parse) **before** writing the ConfigMap, not after deploying it.

**Warning signs:**
`shibd` exit status 254/1 in pod logs immediately after a config change; nginx returns empty-body 502s across every route, not just one app.

**Phase to address:**
Config-rendering phase — add a render-time validation step (parse-and-check, or shell out to `shibd -check`/an XML validator) as a hard gate before the ConfigMap is written, and a unit test asserting no generated comment can contain `--`.

---

### Pitfall 7: Cross-namespace ownerRef garbage collection silently does not fire

**What goes wrong:**
Kubernetes `ownerReferences` are namespace-local by design — a namespaced object cannot own an object in a different namespace. If the operator naively sets an `AppIntegration` (app namespace) as owner of anything in the auth namespace (or vice versa), the owner reference is either rejected or simply never triggers GC, and orphaned resources accumulate silently — no error, no event, just resources that never clean up.

**Why it happens:**
`controllerutil.SetControllerReference` and examples in the Kubebuilder Book assume same-namespace ownership (the common case); it's easy to reach for the same helper across the `SPInstance`/`AppIntegration` namespace boundary without hitting a compile-time or admission-time error — the API server accepts the write, it just never GCs.

**How to avoid:**
Ownership is split strictly by namespace (each controller owns only same-namespace children); everything cross-namespace — the auth-ns `ReferenceGrant`'s `from` list, triggering an SPInstance re-render when an AppIntegration is deleted — is coordinated via **finalizers**, not ownerRefs, exactly as `DESIGN.md` §7 specifies. Add an envtest that deletes an `AppIntegration` and asserts the auth-namespace `ReferenceGrant`/RequestMap entry is actually removed (not just that the finalizer function was called).

**Warning signs:**
`ReferenceGrant` `from` entries or RequestMap fragments for namespaces that no longer have a live `AppIntegration`; deleted CRDs whose cross-namespace side-effects linger.

**Phase to address:**
Reconcile-core phase (both controllers) — cross-namespace finalizer logic is exactly the kind of "must be tested, not just code-reviewed" item; make it a named acceptance test, not an incidental side effect of another test.

---

### Pitfall 8: Non-idempotent reconcile / hot loop from unconditional status writes

**What goes wrong:**
A reconciler that writes `.status` on every invocation — even when nothing changed — bombards the API server, and because a status write bumps `resourceVersion`/triggers a watch event, it can cause the controller to immediately re-queue itself, producing a busy-loop that looks like "the operator is doing something" in metrics/logs but is actually just churning.

**Why it happens:**
It's the path of least resistance to just "always set status at the end of Reconcile" rather than diffing old-vs-new first; with two controllers watching each other's CRDs (`SPInstance` watches `AppIntegration`, `AppIntegration` watches `SPInstance`) an unconditional status write on either side can create a reconcile ping-pong between them.

**How to avoid:**
Compute the desired status, compare with `equality.Semantic.DeepEqual` (or field-by-field) against current status, and only call `Status().Update()`/`Patch()` when it actually differs. Use the `status` subresource (already implied by the CRD design) so status writes don't bump `.metadata.generation`, keeping `observedGeneration`-based staleness detection meaningful. Return the actual error from `Reconcile()` (letting controller-runtime's exponential backoff handle retries) instead of manufacturing `Requeue: true`.

**Warning signs:**
Reconcile count/rate metrics stay elevated with no corresponding spec changes; `kubectl get events` shows a steady drip of "Reconciling" without a triggering diff; CPU/API-server load from the operator doesn't settle after the cluster reaches steady state.

**Phase to address:**
Reconcile-core phase — add a steady-state test: after initial convergence, assert reconcile count for a given object goes to (near-)zero and stays there absent spec changes.

---

### Pitfall 9: Missing field indexers force full-list scans on every reconcile

**What goes wrong:**
`SPInstance`'s controller must aggregate **all** bound `AppIntegration`s to render one RequestMap; `AppIntegration`'s controller must resolve `.spec.spInstanceRef` and `.spec.targetRef`. Without a `client.FieldIndexer` on those reference fields, every such lookup means listing every object of that Kind across the accessible scope and filtering client-side in Go — an O(n) scan repeated on every relevant reconcile/watch event, which gets worse as tenants grow, not better.

**Why it happens:**
It works fine and is invisible in a demo/dev cluster with a handful of CRs; the cost only shows up at fleet scale, by which point the "obvious" list-and-filter code is already load-bearing and harder to refactor under time pressure.

**How to avoid:**
Register field indexers (`mgr.GetFieldIndexer().IndexField(...)`) on `.spec.spInstanceRef` and `.spec.targetRef.name` (already flagged as required in `DESIGN.md` §7) **before the manager starts**, and use `client.MatchingFields` in the map/fan-out functions instead of `List()`+manual filter.

**Warning signs:**
Reconcile latency growing with total `AppIntegration` count rather than staying flat; profiling shows time spent iterating an in-memory list rather than in indexed lookups.

**Phase to address:**
Reconcile-core phase, at initial controller scaffolding — this is cheap to do right from the start and expensive to retrofit; treat as a blocking code-review item, not a later optimization.

---

### Pitfall 10: RequestMap collision handling AND non-deterministic serialization can both cause instability

**What goes wrong:**
Two distinct failure modes stack here. First, if two `AppIntegration`s claim the same `(hostname, path)`, naive last-write-wins render order means whichever reconciled most recently silently wins — a security-relevant nondeterminism (a later, possibly hostile or buggy, app can silently steal another app's route). Second, even with the deterministic-winner logic `DESIGN.md` §7 specifies (oldest `createdAt`, UID tiebreak), if the actual XML serialization iterates a Go `map` anywhere in the render path, key order is randomized per-process — producing a byte-different (but semantically identical) `shibboleth2.xml` on every reconcile, which defeats config-hash-gated rollout (the hash "changes" even though nothing meaningful did) and triggers spurious pod rolls across the whole fleet.

**Why it happens:**
Go intentionally randomizes map iteration order to prevent code from relying on it; a renderer that does `for k, v := range someMap` while building RequestMap/attribute-map XML will produce nondeterministic output unless the keys are explicitly sorted first. This is easy to introduce accidentally (e.g., building an intermediate `map[string]AppFragment` for convenience) even after the collision-winner logic is otherwise correct.

**How to avoid:**
Implement the collision resolution exactly as designed (oldest `createdAt`, UID tiebreak, loser gets `Conflict` + excluded from render — never last-write-wins) **and** ensure every step of the render pipeline sorts by an explicit, stable key (hostname, then path-depth, then AppIntegration UID) before serializing — never range over a Go map when building ordered output that feeds a hash.

**Warning signs:**
The rendered ConfigMap's config-hash annotation changes between two reconciles with no `AppIntegration`/`SPInstance` spec diff (diff the actual XML bytes across two renders in a test to confirm byte-stability); intermittent, hard-to-reproduce "wrong app served this hostname" reports.

**Phase to address:**
Config-rendering phase (RequestMap aggregation) — add a determinism test: render the same input set N times and assert byte-identical output, plus a `Conflict`-condition test for the collision path.

---

### Pitfall 11: XML injection via naive templating of CRD string fields

**What goes wrong:**
Go's `text/template` (the natural first choice for rendering `shibboleth2.xml`) performs **no auto-escaping** — it's designed for trusted templates operating on data the template author controls, not for embedding untrusted values into a structured format. Any CRD-sourced string (an `AppIntegration`'s attribute-header name, a hostname, an entityID, a free-text label) interpolated directly into the XML template can break well-formedness (an unescaped `&`, `<`, or `"`) or, in the worst case, let a value inject an unintended element/attribute — including inadvertently reproducing Pitfall 6's illegal `--`-in-comment FATAL if any rendered value flows into a comment.

**Why it happens:**
`text/template` looks like it "just works" for any text output, and nothing in the standard library warns that XML needs the same escaping discipline HTML gets automatically from `html/template`. Developers coming from web contexts reasonably (but wrongly) assume some equivalent auto-escaping exists for XML.

**How to avoid:**
Either (a) run every interpolated value through `xml.EscapeText` (or an equivalent custom template func registered as the default for every substitution) before it reaches the template, or (b) skip `text/template` for the security-sensitive elements entirely and build them via `encoding/xml` struct marshaling, which escapes correctly by construction. Reject/validate CRD string fields at admission (CRD OpenAPI schema patterns) as defense in depth, but never rely on admission-time validation alone as the injection defense — always escape at render time too.

**Warning signs:**
A config-generation unit test that feeds adversarial strings (`<`, `&`, `"--"`, a literal `]]>`) into every templated CRD field and asserts the output still parses as well-formed XML and preserves the literal value's meaning; any hand-rolled `fmt.Sprintf`-based XML construction in the codebase.

**Phase to address:**
Config-rendering phase — this is the single highest-leverage place to get right early, since every future CRD field addition inherits whichever escaping discipline (or lack of it) is established here. Add an XML-injection fuzz/property test as part of this phase's Nyquist validation.

---

### Pitfall 12: controller-runtime caches the entire Secret type cluster-wide by default — RBAC/blast-radius risk, including the SP private key

**What goes wrong:**
By default, controller-runtime's cache watches and mirrors **every object of a Kind the manager reads**, across every namespace the ServiceAccount can list — including `Secret`, a core type. If the `SPInstance` controller's client isn't scoped down, its informer effectively caches every accessible `Secret` in the cluster in operator memory, and the operator's `ClusterRole` ends up needing broad `get/list/watch` on Secrets to make that work — turning a compromise of the operator pod into a cluster-wide secret-read primitive, which is a much bigger blast radius than "read the one SP keypair Secret it actually needs."

**Why it happens:**
The default, simplest-to-write RBAC (`ClusterRole` with `secrets: get,list,watch`) and default cache config both "just work" without anyone deciding to scope them down; the cost is invisible until a security review or an incident asks "what can this ServiceAccount actually read."

**How to avoid:**
Scope the manager's cache for `Secret` to the auth namespace only (`cache.Options{ByObject: map[client.Object]cache.ByObject{&corev1.Secret{}: {Namespaces: map[string]cache.Config{authNamespace: {}}}}}` or namespace-scoped `Role`+`RoleBinding` instead of `ClusterRole`+`ClusterRoleBinding` for Secrets specifically), and never grant the operator's ServiceAccount `list`/`watch` on Secrets outside the auth namespace. This directly implements the "SP private key not readable per-tenant" constraint already in `PROJECT.md`, but at the RBAC/cache layer, not just the CRD-placement layer — placing `SPInstance` in the auth namespace only prevents tenant *CRDs* from referencing the key; it does nothing to stop the operator's own cache/RBAC from being broader than it needs to be.

**Warning signs:**
`kubectl auth can-i list secrets --as=system:serviceaccount:<ns>:<operator-sa> -A` returns yes; the operator's `ClusterRole` has `secrets` under a cluster-scoped rule rather than a namespaced `Role`.

**Phase to address:**
Reconcile-core / RBAC-scaffolding phase (whichever phase first wires the `SPInstance` controller's Secret access) — this is cheap to get right at scaffold time and should be an explicit code-review checklist item, not discovered later.

---

### Pitfall 13: Config drift when the rendered ConfigMap is edited out-of-band

**What goes wrong:**
Someone (a well-meaning admin debugging in prod, or a script) hand-edits the generated `shibboleth2.xml`/`nginx.conf` ConfigMap directly. If the operator only renders on `AppIntegration`/`SPInstance` spec changes (event-driven reconcile with no periodic resync), the hand-edit persists indefinitely — silently diverging from what the CRDs actually declare — until some unrelated CRD change triggers a re-render and **reverts** the hand-edit, which then looks like a mysterious regression ("it was working, then it broke with no CRD change").

**Why it happens:**
Event-driven-only reconciliation (no periodic full resync, or a resync interval too long to matter operationally) is a reasonable default for efficiency, but leaves a self-healing gap: the whole point of an operator is that the live state should never diverge from source-of-truth without correction, and "no source-of-truth conflict was raised" is not the same as "did not drift."

**How to avoid:**
Configure a periodic resync (controller-runtime's informer resync period, or an explicit periodic requeue) so the ConfigMap is re-rendered and reconciled back to the CRD-derived desired state on a bounded cadence, independent of CRD events. Treat any ConfigMap owned by the operator as fully operator-managed — document (and ideally admission-reject via a webhook or at minimum detect-and-Event) direct edits to operator-owned ConfigMaps.

**Warning signs:**
A hand-diagnosed prod fix "disappears" after an unrelated `AppIntegration` change elsewhere in the cluster; the rendered ConfigMap's content doesn't match what a local dry-run of the renderer produces from the same CRD state.

**Phase to address:**
Rollout & status phase — the config-hash-gated rollout mechanism already planned is the natural place to also add a periodic-resync guarantee; make "no drift after N minutes" part of that phase's acceptance criteria.

---

### Pitfall 14: shibd reload-vs-restart ambiguity leads to the wrong rollout strategy

**What goes wrong:**
Shibboleth SP3's independent config pieces (metadata, attribute-map, and a separately-referenced RequestMapper file) are individually file-monitored and can reload in a background thread without a full `shibd` restart — but this is per-piece, not blanket, and the operator doesn't yet know empirically which `AppIntegration`/`SPInstance` field changes map to a hot-reloadable piece versus one that needs a full pod roll. Assuming "everything reloads in place" risks serving stale config after a change that actually needed a restart; assuming "nothing reloads, always roll" (the safe-but-wasteful default) needlessly churns the whole SP fleet on every trivial AppIntegration edit, which is exactly the "fleet rolls on unrelated reconciles" problem the config-hash gating is meant to prevent in the first place.

**Why it happens:**
The reload-vs-restart boundary is an implementation detail of `shibd`'s `ReloadableConfiguration` mechanism, not something documented as a clean contract — Shibboleth's own docs are explicit that "some changes are picked up automatically, but for others you would have to restart," without enumerating a definitive list per config element.

**How to avoid:**
Treat this as an explicit open question to resolve empirically before committing to a rollout design (it's already flagged as an open gap in `DESIGN.md` §11): test each class of generated-config change (RequestMap-only edit vs. attribute-map edit vs. `SPInstance`-level entityID/credential change) against a running `shibd` and record which classes are safely reload-signaled versus which require a pod roll. Until that's proven, default to the safe choice (roll on any change) gated by the config-hash so at least unrelated reconciles don't trigger it — reload-in-place is an optimization to add once proven, not a correctness requirement.

**Warning signs:**
A RequestMap change doesn't take effect until the next unrelated pod restart; conversely, an attempted "just signal reload" optimization silently serves stale attribute-map data to a subset of pods that didn't pick up the signal.

**Phase to address:**
Rollout & status phase — explicitly scope a research/validation spike (small, targeted — not a full milestone) inside this phase to empirically classify reload-safe vs. restart-required config changes before choosing the default rollout signal.

---

### Pitfall 15: SSRF via IdP federation-metadata fetching

**What goes wrong:**
Any code path — the operator's own metadata-mirror/refresh logic, or `shibd`'s own remote `MetadataProvider` fetch, both of which take a federation-supplied or tenant-supplied URL — that fetches a metadata URL server-side without scheme/IP validation is a Server-Side Request Forgery vector. A real, named advisory in a comparable codebase (openedx-platform, GHSA-64cv-vxpr-j6vc / GHSA-328g-7h4g-r2m9) shows the exact failure: an admin-settable SAML metadata URL fetched with no validation let an authenticated (but lower-trust) actor reach cloud instance-metadata endpoints (`169.254.169.254`) and scan the internal network, escalating to credential theft.

**Why it happens:**
"Fetch this URL and parse the XML" is a one-line-looking operation that doesn't obviously look like a network-boundary-crossing action requiring hardening, especially when the URL comes from a config field (an `SPInstance`'s federation-metadata source) rather than obviously-untrusted end-user input.

**How to avoid:**
If the operator ever implements its own metadata-mirror/refresh (rather than only handing the URL to `shibd`), validate the URL before fetching: enforce `https`, block loopback/link-local/reserved/RFC1918 ranges by default (with an explicit opt-in override for legitimate same-cluster/private IdP deployments), and set a request timeout. Even where `shibd` itself does the fetching rather than the operator, this is worth surfacing to whoever authors `SPInstance`s: the federation-metadata-source field is effectively an SSRF-shaped input, and in an egress-restricted cluster (already a known constraint) the operator-managed in-cluster metadata mirror mentioned in `DESIGN.md` is exactly the kind of code that must apply this validation.

**Warning signs:**
Any operator code that calls an HTTP client with a URL sourced from a CRD field and no scheme/host allowlist; a metadata-source field with no CRD-level validation pattern restricting scheme or disallowing IP-literal hosts.

**Phase to address:**
Whichever phase implements the in-cluster metadata mirror/refresh (a `DESIGN.md` §11 open gap, likely a later phase than the v1 operator) — flag as a hard requirement in that phase's spec rather than an afterthought, since it's a well-precedented CVE class in this exact feature shape.

---

### Pitfall 16: Authenticator Service reachable outside the gateway makes `X-Forwarded-Host` spoofable

**What goes wrong:**
The whole RequestMapper/authorization decision keys on `X-Forwarded-Host` (and related `X-Forwarded-*` headers) reconstructed from the gateway's forward-auth call. If the authenticator Service (or its headless counterpart, per Pitfall/Fix 3) is reachable from anywhere other than the gateway — no NetworkPolicy restricting ingress, or a NetworkPolicy that's declared but not actually enforced by the CNI in a given environment — any pod in the cluster can call `/authcheck` directly with a forged `X-Forwarded-Host`, bypassing the gateway's own header hygiene entirely and impersonating any hostname's applicationId.

**Why it happens:**
This is a design **constraint** already correctly identified in `DESIGN.md`/`PROJECT.md` ("must be reachable only from the gateway"), but a constraint documented in prose is not the same as a constraint the operator actually enforces at deploy time — if rendering the restricting NetworkPolicy is left as a manual, cluster-admin-applied step (as it currently is in the spike, where NetworkPolicies are spike-only permissive scaffolding) rather than something the operator generates and reconciles per `SPInstance`/`AppIntegration`, it's tribal knowledge that will eventually not get applied in some environment.

**How to avoid:**
The operator should generate and continuously reconcile a NetworkPolicy restricting ingress to the authenticator Service/pods to only the gateway's namespace (mirroring the spike's `allow-traefik-ingress`, generalized and operator-owned rather than hand-applied), and treat "NetworkPolicy present and correct" as a status condition the operator can report, not an assumption. Additionally verify empirically per target CNI that NetworkPolicy is actually enforced (some CNI configurations silently no-op it) — don't treat "the YAML exists" as "the control is enforced."

**Warning signs:**
No `NetworkPolicy` object owned/reconciled by the operator restricting the authenticator Service's ingress; a security review that can reach `/authcheck` directly from an arbitrary in-cluster pod and successfully spoof `X-Forwarded-Host`.

**Phase to address:**
Gateway-attachment phase (alongside the headless-Service/Middleware rendering) — make NetworkPolicy generation a required deliverable of this phase, not an out-of-band cluster-admin task, and cover it with a Nyquist-style verification (an in-cluster pod that is NOT the gateway attempting to reach `/authcheck` directly, asserting it's blocked).

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|--------------------|-----------------|------------------|
| Dumb static-200 readiness probe (spike-era) | Fast to stand up, unblocks early testing | Masks `shibd` FATAL, routes traffic to a dead SP (Pitfall 5) | Never past the spike/dev stage — must be replaced before any shared/multi-tenant use |
| Skip config-hash gating, roll the SP Deployment on every reconcile | Simpler rollout code initially | Any unrelated `AppIntegration` edit anywhere churns the whole SP fleet, causing avoidable session/latency disruption at scale | Acceptable only for a single-tenant MVP with one `AppIntegration`; must be fixed before multi-tenant use |
| Hardcode the Traefik `Middleware`/ForwardAuth attachment with no abstraction layer | Faster to ship v1, matches the one gateway currently in use | Locks the operator to Traefik; GEP-1494/Envoy support becomes a rewrite instead of a new adapter | Acceptable for v1 **if** the attachment-rendering code is isolated behind a clear internal interface from day one (cheap insurance even while there's only one implementation) |
| Event-driven-only reconcile, no periodic resync | Lower steady-state API load | Config drift from out-of-band edits persists indefinitely (Pitfall 13) | Acceptable only if a periodic resync is added before the operator is used in any environment where humans can touch the ConfigMap directly |
| ClusterRole with broad Secret access instead of namespace-scoped Role + scoped cache | Less RBAC/cache-config boilerplate to write | Operator compromise = cluster-wide secret read, not just the one keypair (Pitfall 12) | Never — this is cheap to do correctly from the start; not a "fix later" item |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| Traefik Gateway API + Middleware CRD | Enabling only the `kubernetesGateway` provider; the `ExtensionRef` filter on the HTTPRoute pointing at a `traefik.io` Middleware silently fails to resolve without the `kubernetesCRD` provider also enabled | Document/verify both Traefik providers (`kubernetesGateway` + `kubernetesCRD`) are enabled as a precondition; add a status condition if the operator can detect the Middleware never got picked up |
| ReferenceGrant vs. attachment model | Assuming every attachment model needs a `ReferenceGrant` for the cross-namespace hop | Traefik's ForwardAuth-by-URL sidesteps `ReferenceGrant` entirely (it's not a `backendRef`); only attachment types using a real `backendRef` (future GEP-1494/Envoy) need it — don't over-generalize one model's requirements onto another |
| Remote federation metadata (InCommon) in egress-restricted clusters | Assuming `shibd`'s remote `MetadataProvider` will "just work" in production the way it did once egress was opened for the spike | Egress-restricted clusters need either an explicit egress allowlist for the metadata endpoint or an in-cluster metadata mirror the operator manages and refreshes (with SSRF-hardening per Pitfall 15) |
| `shibd` config reload signaling | Assuming a ConfigMap update automatically triggers a reload the same way a locally-edited file does on a normal filesystem | ConfigMap-mounted files update via kubelet's periodic sync (not instantaneous, and not the same as the file-watch shibd expects for `ReloadableConfiguration`) — verify the propagation-to-reload path empirically, don't assume it mirrors bare-metal behavior |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| No field indexers on `.spec.spInstanceRef`/`.spec.targetRef` | Reconcile latency grows with total `AppIntegration` count; CPU time in list+filter loops | Register field indexers before manager start; use `client.MatchingFields` | Noticeable once tenant count moves from a handful to dozens+ per `SPInstance` |
| RequestMap re-render is O(all bound AppIntegrations) on every single AppIntegration reconcile | Every trivial per-tenant edit re-walks and re-renders the entire fleet's RequestMap | Cache/memoize the aggregation where possible; ensure config-hash gating prevents a redundant *rollout* even if the render itself re-runs | Becomes visible as reconcile-queue latency once dozens of `AppIntegration`s share one `SPInstance` |
| Controller-runtime cache watching `Secret` cluster-wide | Elevated operator memory footprint proportional to *every* cluster Secret, not just the ones it needs | Scope cache `ByObject` namespaces for `Secret` to the auth namespace | Becomes a real memory line item in clusters with many unrelated Secrets across many namespaces |
| Unbounded reconcile requeue period during transient failures (e.g., waiting on remote metadata fetch) | Tight requeue loop hammering an external IdP/metadata endpoint under sustained failure | Exponential backoff (controller-runtime's default via returned errors) rather than a fixed short `RequeueAfter` | Becomes an availability/rate-limit problem against the external IdP once a failure is sustained for minutes+ |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Authenticator Service reachable from outside the gateway | `X-Forwarded-Host` spoofable → any in-cluster pod can impersonate any hostname's applicationId, bypassing the entire auth model | Operator-generated, reconciled NetworkPolicy restricting ingress to the gateway namespace only; verify enforcement empirically per CNI (Pitfall 16) |
| SP private key readable outside the auth namespace | Any tenant with namespace access could exfiltrate the signing/encryption key, forging assertions or decrypting sessions | Keep `SPInstance`/keypair Secret in the auth namespace only, **and** scope the operator's own RBAC/cache for Secrets to that namespace (not just CRD placement) — Pitfall 12 |
| CRD schema permits a tenant `AppIntegration` to set an authz-weakening field (e.g., disable `requireSession`, widen `allowedNamespaces` consent) with no admission-time guard | A single compromised/careless tenant namespace can unprotect its own route, or worse, if the field has broader blast radius, someone else's | Model authz-sensitive fields as immutable-after-creation or centrally-owned where possible; add CEL `ValidatingAdmissionPolicy`/webhook validation for fields that must never widen without SPInstance-side consent, matching the existing `allowedNamespaces` consent pattern in `DESIGN.md` §5 |
| SSRF via a federation-metadata URL field | Cloud-metadata-endpoint access, internal network scanning, credential theft (real precedent: openedx GHSA-64cv-vxpr-j6vc) | Validate scheme + block loopback/link-local/reserved/RFC1918 by default wherever the operator itself fetches a metadata URL; document the risk even where `shibd` does the fetching (Pitfall 15) |
| Operator ServiceAccount over-scoped (ClusterRole where a Role would do) | Operator-pod compromise yields cluster-wide read/write beyond what the operator's actual job requires | Namespace-scoped `Role`/`RoleBinding` wherever the operator only ever needs one namespace (e.g., Secrets in the auth namespace); reserve `ClusterRole` for genuinely cluster-scoped needs (CRD watches across namespaces) |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|--------------|-------------------|
| `AppIntegration` surfaces only a raw `Conflict`/`Degraded` condition with no actionable detail | App team sees "Degraded: true" and has to go read operator logs or source to know *why* (which other AppIntegration won the collision, which HTTPRoute match wasn't derivable) | Put the specific cause and the losing/winning identity directly in the condition `message` (e.g., "path /foo already claimed by AppIntegration other-app (created earlier); this AppIntegration excluded from RequestMap") |
| SP metadata URL not surfaced anywhere until an app team goes hunting | IdP-registration handoff (a real external dependency with lead time, per `DESIGN.md` §11) stalls on someone finding the right URL manually | Already an Active requirement (`SPInstance` status exposes the metadata URL) — keep this visible and copy-pasteable directly from `kubectl describe`/status, not buried |
| Header/method-based HTTPRoute matches silently can't be expressed in the RequestMapper | App team writes a header-based HTTPRoute rule expecting it to be protected, and it silently isn't (or the whole `AppIntegration` fails validation with a message they don't map back to their HTTPRoute rule) | Surface as a specific, named `Degraded` condition ("route rule N uses header/method matching, not supported by the RequestMapper — host/path matching only") pointing at the exact unsupported rule |

## "Looks Done But Isn't" Checklist

- [ ] **Reconciler compiles + envtest is green:** doesn't mean cross-namespace GC actually works — verify with a test that deletes an `AppIntegration` and asserts the auth-namespace `ReferenceGrant`/RequestMap fragment is actually removed (Pitfall 7).
- [ ] **Config renders valid-looking XML:** doesn't mean it's injection-safe — verify with adversarial CRD field values (`<`, `&`, `"--"`, `]]>`) fed through the renderer, asserting well-formed output every time (Pitfall 11).
- [ ] **ForwardAuth allow/deny works in local/dev testing:** doesn't mean it survives the real gateway/CNI — verify the emitted address is the headless Service, not the ClusterIP, and re-test after any CNI change (Pitfall 3).
- [ ] **Readiness probe returns 200:** doesn't mean `shibd` is healthy — verify readiness flips to NotReady when `shibd` is killed inside a running pod (Pitfall 5).
- [ ] **RequestMap looks correct in a quick manual test:** doesn't mean it's deterministic under concurrent creates or byte-stable across reconciles — verify with a repeated-render determinism test and an explicit two-AppIntegration collision test (Pitfall 10).
- [ ] **Attribute headers reach the app correctly:** doesn't mean hygiene is closed — verify an *unlisted* `Variable-*` header injected by the client is also confirmed NOT reaching the app (not just that listed ones are overwritten) (Pitfall 4).
- [ ] **A NetworkPolicy YAML exists restricting the authenticator Service:** doesn't mean it's enforced — verify empirically, from an in-cluster pod that is not the gateway, that `/authcheck` is actually unreachable (Pitfall 16).
- [ ] **The operator's RBAC manifest was generated by kubebuilder markers and "looks standard":** doesn't mean it's appropriately scoped — verify Secret access is namespace-scoped, not cluster-wide (Pitfall 12).

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|-----------------|------------------|
| RequestMap fails open (Pitfall 1) discovered in a live environment | MEDIUM–HIGH | Patch the renderer to always emit scheme+port, redeploy immediately, then audit access logs for the exposure window to determine if the open path was actually hit by unauthenticated traffic |
| SP private key over-exposed via broad RBAC/cache scope (Pitfall 12) | HIGH | Rotate the SP keypair, re-publish/re-register updated metadata with every relying IdP/federation (external lead time — treat as an incident, not a quiet fix), then scope RBAC/cache correctly before considering it closed |
| Unlisted `Variable-*` header leak (Pitfall 4) found in production | MEDIUM | Ship the enumerate-clear Middleware update or the wildcard-strip plugin immediately for the affected apps; for apps that can't wait, migrate them to the nginx-`auth_request` edge model which closes the gap structurally |
| Config drift silently reverted a hand-applied prod fix (Pitfall 13) | LOW–MEDIUM | Add the periodic resync; in the interim, treat any manual ConfigMap edit as temporary and immediately follow up with the correct CRD-level change so the next reconcile doesn't revert real intent |
| Non-deterministic RequestMap serialization causing rollout thrash (Pitfall 10) | LOW | Add explicit sort-by-key before serialization at every render step; add the byte-stability regression test so it can't silently return |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|----------------|
| RequestMap fails open without scheme+port (1) | Config-rendering phase | Negative test: unauthenticated hit to a non-standard-port protected route returns non-200 |
| `SHIBSP_SERVER_*` env drives self-URL (2) | Config-rendering / Deployment-rendering phase | Unit test asserting env vars + `handlerURL` always consistent |
| ForwardAuth targets headless Service (3) | Gateway-attachment phase | E2e assertion the emitted address is the headless FQDN, not ClusterIP |
| Unlisted `Variable-*` header leak (4) | Gateway-attachment phase | Spoof-injection test: unlisted header from client must not reach app |
| Dumb readiness masks shibd FATAL (5) | Rollout & status phase | Chaos test: kill `shibd` in-pod, assert readiness flips |
| Illegal `--` in XML comments (6) | Config-rendering phase | Render-time XML validation gate + unit test rejecting `--` in generated comments |
| Cross-namespace ownerRef GC trap (7) | Reconcile-core phase | Envtest: delete AppIntegration, assert auth-ns cross-ns side effects actually removed |
| Non-idempotent reconcile / status hot loop (8) | Reconcile-core phase | Steady-state test: reconcile count converges to ~0 absent spec changes |
| Missing field indexers → full scans (9) | Reconcile-core phase (scaffold time) | Latency-vs-object-count benchmark stays flat |
| Collision handling + nondeterministic serialization (10) | Config-rendering phase | Repeated-render byte-stability test + two-AppIntegration collision test |
| XML injection via templating (11) | Config-rendering phase | Adversarial-input fuzz test asserting well-formed output |
| Cluster-wide Secret cache/RBAC (12) | Reconcile-core / RBAC scaffolding phase | RBAC audit (`kubectl auth can-i` check) scoped to auth namespace only |
| Config drift from out-of-band edits (13) | Rollout & status phase | Drift test: hand-edit ConfigMap, confirm it's corrected within the resync bound |
| shibd reload vs restart ambiguity (14) | Rollout & status phase | Empirical classification spike of which config classes hot-reload vs. need a roll |
| SSRF via metadata URL fetch (15) | Metadata-mirror phase (later/DESIGN §11 gap) | URL-validation unit test blocking loopback/link-local/RFC1918 by default |
| Authenticator Service reachable outside gateway (16) | Gateway-attachment phase | In-cluster non-gateway pod attempts direct `/authcheck` call, must be blocked |

## Sources

- `DESIGN.md` and `.planning/threads/saml-sp-operator.md` (this repo) — the six MUST-CARRY-FORWARD spike-proven pitfalls, first-party, HIGH confidence.
- [Good Practices — The Kubebuilder Book](https://book.kubebuilder.io/reference/good-practices) — idempotency, status-subresource, CreateOrUpdate guidance. MEDIUM confidence (official project docs).
- [So you wanna write Kubernetes controllers? — Ahmet Alp Balkan](https://ahmet.im/blog/controller-pitfalls/) — CRD design, spec/status separation, informer/expectations-pattern pitfalls. MEDIUM confidence (well-known, widely-cited community source).
- [misunderstanding of the documentation (IndexField) · controller-runtime#1941](https://github.com/kubernetes-sigs/controller-runtime/issues/1941) and [Understanding the controller-runtime Cache Seriously](https://dev.to/shuheiktgw/understanding-the-controller-runtime-cache-seriously-3c2k) — field-indexer and cache-scoping behavior. MEDIUM confidence.
- [`html/template` vs `text/template` — Go Packages](https://pkg.go.dev/text/template) and [Safe by Default or Vulnerable by Design? Golang SSTI](https://www.oligo.security/blog/safe-by-default-or-vulnerable-by-design-golang-server-side-template-injection) — no-auto-escaping behavior of `text/template`. MEDIUM confidence.
- [SSRF via SAML metadata URL in sync_provider_data endpoint — GHSA-64cv-vxpr-j6vc](https://github.com/openedx/edx-enterprise/security/advisories/GHSA-64cv-vxpr-j6vc) and [GHSA-328g-7h4g-r2m9](https://github.com/openedx/openedx-platform/security/advisories/GHSA-328g-7h4g-r2m9) — real-world SAML-metadata-fetch SSRF precedent. MEDIUM-HIGH confidence (named CVE-class advisory, directly on-topic).
- [ReloadableConfiguration — Shibboleth SP3 Confluence](https://shibboleth.atlassian.net/wiki/spaces/SP3/pages/2063695928/ReloadableConfiguration) — per-piece hot-reload behavior and its caveats. MEDIUM confidence (official Shibboleth project wiki).

---
*Pitfalls research for: Kubernetes operator generating a Shibboleth SAML SP + gateway forward-auth configuration*
*Researched: 2026-07-09*
