# Stack Research

**Domain:** Kubernetes operator (Go / kubebuilder / controller-runtime) — config-rendering + reconciliation layer over a containerized Shibboleth SP v3
**Researched:** 2026-07-09
**Confidence:** HIGH (versions verified against pkg.go.dev / official repos, not training-data recall) — see per-row notes for the few MEDIUM/LOW judgment calls

**Scope note:** DESIGN.md §1–§12 and PROJECT.md already fix the architecture (Go + kubebuilder/controller-runtime, containerized SP v3, two CRDs, Traefik ForwardAuth, shared memcached). This file does **not** revisit those decisions. It covers only the implementation stack the operator needs *beyond* the kubebuilder scaffold: config templating, config-hash/rollout gating, controller-runtime helper APIs, Gateway API + Traefik CRD Go types, and rendering-correctness testing.

## What's Already in the Scaffold (branch `gsd/operator-scaffold`, verified from `go.mod`)

| Technology | Scaffold version | Latest verified | Action |
|------------|-------------------|------------------|--------|
| Go toolchain | 1.26.0 | — | keep |
| `sigs.k8s.io/controller-runtime` | v0.24.1 | **v0.24.1** (pkg.go.dev versions tab, confirmed current) | keep, no bump |
| `k8s.io/apimachinery`, `k8s.io/client-go` | v0.36.0 | v0.36.2 exists (pulled in transitively by gateway-api, see below) | no action — Go module MVS will resolve to v0.36.2+ automatically once gateway-api is added; don't hand-pin |
| `github.com/onsi/ginkgo/v2` | v2.27.4 | **v2.32.0** | bump — low risk, do it opportunistically, not blocking |
| `github.com/onsi/gomega` | v1.39.0 | **v1.42.1** | bump alongside ginkgo |
| kubebuilder CLI | v4.15.0 (installed, not a go.mod dep) | — | keep |
| envtest (via `sigs.k8s.io/controller-runtime/tools/setup-envtest`) | not yet run | — | pin the envtest K8s binary version to the same minor as the target cluster's control plane when `make test` is first run; controller-runtime v0.24.1 supports recent K8s minors including 1.36 |

**Bottom line: the scaffold's core versions are already current.** The only scaffold action item is a routine ginkgo/gomega minor bump. Everything below is genuinely new surface to add.

## Recommended Additions

### Config Templating (shibboleth2.xml / attribute-map.xml / nginx.conf)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|------------------|
| `encoding/xml` (stdlib) | Go 1.26 stdlib | Render `shibboleth2.xml` and `attribute-map.xml` from typed Go structs, not string templates | The RequestMap the operator builds (DESIGN §7 step 5) is a **tree with strict ordering/nesting rules** — collision-resolved hostnames, exact-before-wildcard, most-specific-path-first, nested `<Path>` from segments. That's naturally expressed as Go structs built by ordinary Go code (sort, build, marshal), not `text/template` range/if control flow trying to fake tree construction in a text DSL. It also gets XML-escaping for free (`xml.Marshal` escapes all string content correctly), closing the injection-shaped risk that string-interpolated templates require you to remember on every field. |
| `text/template` (stdlib) | Go 1.26 stdlib | Render `nginx.conf` (line-oriented, not tree-shaped) | nginx config is directive-per-line, not attribute-order-sensitive XML — `text/template` is the right tool there. Keep the func map tiny and hand-written (2–3 helpers: e.g. an `indent` and a defensive quoting helper for any value that could contain nginx-special characters) rather than pulling in a general-purpose helper library for a handful of functions. |

**What NOT to use for templating:**

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `text/template` with string interpolation for `shibboleth2.xml` / `attribute-map.xml` | The spike's own fix-K and fix-N regressions were exactly this class of bug — a literal `--` inside an XML comment silently killed shibd parsing twice, and this is the general risk of hand-assembling XML as text. A tree-shaped, ordering-sensitive document (RequestMap) is also awkward to express correctly in a text template. | `encoding/xml.Marshal`/`MarshalIndent` over Go structs mirroring the shibboleth2.xml schema |
| `html/template` for any of these files | Its contextual auto-escaping is designed for HTML/JS/CSS/URL contexts embedded in a browser response — it either double-escapes or escapes the wrong characters for XML attribute/text content and for nginx directives, and offers no correctness benefit here since none of this output reaches a browser. | `text/template` (nginx.conf) or `encoding/xml` (XML files) |
| `Masterminds/sprig` (v3.3.0, confirmed current) | 100+ template functions to get 2–3 you'll actually use (`indent`, maybe a default/quote helper); adds a real dependency + its own transitive graph for something a 15-line local `template.FuncMap` covers completely. | Hand-write the handful of needed helpers in `internal/render/funcs.go` |
| Any XSD-schema-validating library for `shibboleth2.xml` (e.g. `terminalstatic/go-xsd-validate`, `lestrrat-go/libxml2`) | These bind to C `libxml2`/`libxslt` via cgo — the *exact same risk class* DESIGN §2 already rejected for `samael`/`xmlsec1` (unaudited C glue in the bypass-prone code path). Pulling a cgo XML validator into the operator to check config **we generate** reintroduces that risk for a component that isn't even doing SAML crypto. | Golden-file tests (below) for regression detection, **plus** the spike's own hard-learned validation method: load the rendered config into a real (containerized) `shibd` as a CI/integration-test step — that's the authoritative parser, and fixes K/L/N in the spike were only ever caught this way. |

### Config-Hash / Rollout Gating

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|------------------|
| `crypto/sha256` + `encoding/hex` (stdlib) | Go 1.26 stdlib | Hash the final rendered config bytes (concatenated or per-file) and stamp the result as a pod-template annotation (e.g. `saml.tickletechnologies.com/config-hash`) on the SP Deployment | This is exactly DESIGN §7 step 5: "Serialize, hash, stamp as a pod-template annotation; only roll when the hash changes." Hashing the **rendered output bytes**, not the CRD spec structs, is what actually determines whether the Deployment needs to roll, and it's naturally deterministic because both the XML struct marshal and the nginx text/template render are deterministic. No external library adds anything here. |

**What NOT to use:**

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `k8s.io/kubectl/pkg/util/hash.DeepHashObject` (the FNV hash the Deployment controller uses internally for `pod-template-hash`) | Importing it drags in `k8s.io/kubectl` — a large fraction of the kubectl codebase — as a dependency of an operator binary that has no other reason to need it. `DeepHashObject` also hashes a Go object via `%#v`-style dumping, which is a worse fit than hashing the actual rendered config bytes you already have in hand. | `crypto/sha256` over the rendered bytes |

### Gateway API Go Types

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|------------------|
| `sigs.k8s.io/gateway-api` | **v1.6.0** (confirmed current on pkg.go.dev, June 2026) | Go types for reading `HTTPRoute` (and any Gateway/Listener introspection needed for the hostname-claim VAP work in DESIGN §9) | HTTPRoute is GA in the `v1` package (`sigs.k8s.io/gateway-api/apis/v1`) as of Gateway API 1.4+; the operator only ever *reads* HTTPRoutes (resolve `spec.hostnames`, `spec.rules[].matches[].path`), so import only `apis/v1`, not `apisx` (experimental) or the conformance/test packages. gateway-api's own `go.mod` requires `k8s.io/apimachinery`/`k8s.io/client-go` v0.36.2 — compatible with the scaffold's v0.36.0 (Go's MVS will pick v0.36.2, no conflict). |

### Traefik CRD Go Types — hand-roll, don't import `traefik/traefik`

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|------------------|
| A small **hand-rolled local types package** (e.g. `internal/traefik/v1alpha1`) containing only `Middleware`, `MiddlewareSpec`, `ForwardAuth`, and (for the header-clear middleware) `Headers` | n/a — owned code, versioned with the operator | Emit the Traefik `Middleware` CRD without importing the upstream Traefik module | **Verified finding, not a guess:** the only place Traefik publishes these Go types is inside its own binary's module, `github.com/traefik/traefik/v3` — there is no separate lightweight "traefik-crd-types" module. That module's `go.mod` declares **~95 direct dependencies** (cloud provider SDKs, DNS providers, OpenTelemetry, etc.) — it's the whole reverse-proxy binary, not a types library. Even though Go only compiles packages actually imported, adding this module to `go.mod`/`go.sum` still pulls its full dependency graph into version resolution and the supply-chain/audit surface, and couples the operator's Go module graph to Traefik's release cadence for a type the operator uses maybe two fields of (`ForwardAuth.Address`, `ForwardAuth.AuthResponseHeaders`). The Middleware CRD schema is small, stable, and documented (doc.traefik.io Middleware reference); mirroring just the fields the operator emits is the same "swappable-engine" discipline DESIGN §9 already applies to the attachment layer generally (Traefik now, GEP-1494/Envoy later) — the attachment CRD type should be exactly as swappable as the Middleware you emit for it. |

**Confidence on this one:** MEDIUM — it's an architectural judgment call (not an official "don't do this" doc from Traefik), but it's backed by a directly measured fact (the ~95-dependency count), and it's consistent with the project's own established pattern of avoiding unaudited/heavyweight third-party glue in a component that doesn't need it (DESIGN §2's `samael`/`xmlsec1` rejection). If Jeff prefers official types over hand-rolled for correctness assurance instead, the fallback is importing only `github.com/traefik/traefik/v3/pkg/config/dynamic` (a narrower subpackage than the full CRD provider) — cross-check both approaches against the real struct before locking in.

**What NOT to do:**

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `import "github.com/traefik/traefik/v3/pkg/provider/kubernetes/crd/traefikio/v1alpha1"` (or the v2 path `.../crd/traefik/v1alpha1`) as a go.mod dependency | Full upstream module, ~95 direct deps, version-coupled to Traefik's own release cycle | Hand-rolled minimal local types (above), field/JSON-tag-accurate cross-checked against Traefik's official Middleware CRD reference docs |

### Controller-Runtime Patterns (current v0.24.1 APIs — verified, not assumed from older tutorials)

| API | Package | Signature (v0.24.1, verified via pkg.go.dev) | Use For |
|-----|---------|------------------------------------------------|---------|
| Status conditions | `k8s.io/apimachinery/pkg/api/meta` | `SetStatusCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) bool`; `FindStatusCondition`, `IsStatusConditionTrue`, `IsStatusConditionFalse`, `IsStatusConditionPresentAndEqual`, `RemoveStatusCondition` | The `SPInstanceResolved`/`RouteResolved`/`Conflict`/`Degraded`/`Ready`/`ConfigRendered` conditions DESIGN §7 requires on both CRDs. These have been stable since apimachinery v0.19 — this is the canonical helper set, don't hand-roll condition-slice mutation. |
| Finalizers | `sigs.k8s.io/controller-runtime/pkg/controller/controllerutil` | `AddFinalizer(o client.Object, finalizer string) bool`; `RemoveFinalizer(o client.Object, finalizer string) bool`; `ContainsFinalizer(o client.Object, finalizer string) bool` | The cross-namespace coordination DESIGN §7 mandates in place of ownerRefs (AppIntegration finalizer maintaining its entry in the auth-ns ReferenceGrant's `from` list). |
| Watches / Owns | `sigs.k8s.io/controller-runtime/pkg/builder` | `Watches(object client.Object, eventHandler handler.TypedEventHandler[client.Object, request], opts ...WatchesOption)`; `Owns(object client.Object, opts ...OwnsOption)` | Both take a `client.Object` directly now — **the older `source.Kind{Type: ...}` + manual `handler.EnqueueRequestForOwner{...}` pattern from pre-v0.16 tutorials/blog posts is obsolete for this.** Don't copy that shape from older Medium posts/StackOverflow answers; the current builder API is simpler and is what kubebuilder v4.15 scaffolds already emit. |
| Field indexers | `sigs.k8s.io/controller-runtime/pkg/manager` (via `mgr.GetFieldIndexer()`) | `IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error` | The `.spec.spInstanceRef` and `.spec.targetRef.name` indexes DESIGN §7 calls for, feeding the fan-out map functions for `Watches`. |
| Idempotent create/update | `sigs.k8s.io/controller-runtime/pkg/controller/controllerutil` | `CreateOrUpdate(ctx, c client.Client, obj client.Object, mutate MutateFn) (OperationResult, error)` | Handy for the SPInstance controller's owned same-namespace objects (Deployment/ConfigMap/Service) — avoids hand-writing get-then-create-or-update branches for each. |

### Rendering-Correctness Testing

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|------------------|
| `github.com/google/go-cmp` | **v0.7.0** (already resolved transitively in the scaffold's `go.sum` as indirect; promote to a direct test dependency, no version change needed) | Struct/semantic diffing in unit tests comparing rendered Go structs (pre-marshal) against expected fixtures | Since it's already pulled in indirectly by the k8s.io test tooling, adding it as a direct import costs nothing new in the dependency graph — just move it from `// indirect` to a normal `require` on next `go mod tidy`. |
| Golden-file pattern (hand-rolled, no library) | n/a | `testdata/*.golden` fixtures for the fully-rendered `shibboleth2.xml`, `attribute-map.xml`, and `nginx.conf`, with a `-update` test flag to regenerate them | Standard Go idiom (used throughout the Go stdlib and most major Go projects) — a `flag.Bool("update", false, ...)`-gated compare-or-write function is ~15 lines and needs no dependency. Don't add `github.com/sebdah/goldie` or similar for this; it buys nothing over the hand-rolled version for three fixture files. |
| envtest + ginkgo/gomega (already scaffolded) | v0.24.1 / v2.32.0 / v1.42.1 | Integration tests: full reconcile loop against a real (envtest) API server, asserting the Deployment/ConfigMap/Service/conditions end state | Already the kubebuilder-standard pattern; no change needed beyond the ginkgo/gomega bump above. |
| Rendered-config parseability check via real `shibd` (from the spike, not a new library) | n/a — reuse the spike's containerized SP image | An integration/CI step (or a targeted test in the same style as the spike's local Docker repro) that loads the rendered `shibboleth2.xml` into an actual `shibd` process and asserts clean startup (all procs RUNNING, no FATAL) | This is the **authoritative validation** for the one thing golden-file text-compare *cannot* catch: whether shibd's own real XML parser accepts the config. The spike hit this exact gap twice (fix K, fix N regression) — a byte-perfect-looking golden file can still be XML-invalid (illegal `--` in a comment) or semantically wrong (RequestMap key that fails open) in ways only real `shibd` parsing/behavior exposes. Treat this as a mandatory rendering-correctness gate for the shibboleth2.xml renderer, not optional. |

## Installation

```bash
# New direct dependencies to add
go get sigs.k8s.io/gateway-api@v1.6.0
go get github.com/google/go-cmp@v0.7.0   # promote existing indirect dep to direct

# Routine version bumps (not blocking, do opportunistically)
go get github.com/onsi/ginkgo/v2@v2.32.0
go get github.com/onsi/gomega@v1.42.1

go mod tidy
```

No new templating/hashing/finalizer/condition libraries are needed — all of that is stdlib (`encoding/xml`, `text/template`, `crypto/sha256`) or already-present controller-runtime/apimachinery helpers.

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|--------------|-------------|--------------------------|
| `encoding/xml` struct marshaling for shibboleth2.xml | `text/template` with an `xmlEscape` FuncMap helper | If the shibboleth2.xml shape ever needs to interpolate large blocks of pre-formatted/untyped XML fragments that don't map cleanly to a Go struct (unlikely given DESIGN §5–§7's schema is well-understood) |
| Hand-rolled minimal Traefik types | `github.com/traefik/traefik/v3/pkg/config/dynamic` only (narrower than the full CRD provider package) | If Jeff weighs "official, always-in-sync-with-Traefik field names" above "minimal dependency footprint" — still lighter than the full CRD/provider package, but still couples to Traefik's release cadence |
| Traefik `Middleware` CRD via typed hand-rolled Go structs | `unstructured.Unstructured` + raw map construction | If the operator ends up needing to emit many different Traefik CRD kinds with frequently-changing schemas — unstructured avoids maintaining N hand-rolled type packages, at the cost of losing compile-time field safety. Not warranted yet: DESIGN currently needs exactly one Traefik CRD (Middleware). |
| `crypto/sha256` over rendered bytes | `hash/fnv` (also stdlib, what the Deployment controller itself uses internally, just not via the heavy `k8s.io/kubectl` import) | If annotation-value length becomes a real concern and a shorter, non-cryptographic hash is preferred — FNV-1a is fine for change-detection (not a security boundary); either is a defensible stdlib choice, sha256 is just the more conventional operator idiom |

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-------------------|-------|
| `sigs.k8s.io/controller-runtime@v0.24.1` | `k8s.io/apimachinery@v0.36.0`, `k8s.io/client-go@v0.36.0` | Already resolved and green in the scaffold (`make build` + envtest passing per thread notes) |
| `sigs.k8s.io/gateway-api@v1.6.0` | `k8s.io/apimachinery@v0.36.2`, `k8s.io/client-go@v0.36.2` | One patch ahead of the scaffold's v0.36.0 pin — Go's MVS resolves this automatically on `go get`/`go mod tidy`, no manual pin needed and no known breaking changes between v0.36.0→v0.36.2 |
| `github.com/onsi/ginkgo/v2@v2.32.0` | `github.com/onsi/gomega@v1.42.1` | Bump both together; scaffold's existing v2.27.4/v1.39.0 pairing is not a compatibility problem, just behind |

## Sources

- pkg.go.dev — `sigs.k8s.io/controller-runtime` versions tab (HIGH confidence, official Go module index) — confirmed v0.24.1 is current
- pkg.go.dev — `sigs.k8s.io/gateway-api` versions tab (HIGH confidence) — confirmed v1.6.0 current
- pkg.go.dev — `sigs.k8s.io/controller-runtime@v0.24.1/pkg/builder`, `/pkg/controller/controllerutil`, `k8s.io/apimachinery@v0.36.0/pkg/api/meta` (HIGH confidence, exact signatures fetched directly)
- pkg.go.dev — `github.com/onsi/ginkgo/v2`, `github.com/onsi/gomega`, `github.com/google/go-cmp` versions tabs (HIGH confidence)
- `github.com/traefik/traefik` `go.mod` (raw GitHub, master branch) — direct measurement of ~95 direct dependencies (HIGH confidence on the count; MEDIUM confidence on the resulting hand-roll-vs-import recommendation, which is this researcher's judgment)
- `github.com/traefik/traefik/pkg/provider/kubernetes/crd/traefikio/v1alpha1/middleware.go` (raw GitHub) — confirmed `Middleware`/`MiddlewareSpec` struct shape and its import of `pkg/config/dynamic`
- `kubernetes-sigs/gateway-api` `go.mod` (raw GitHub, main branch) — confirmed apimachinery/client-go v0.36.2 requirement
- Web search — Go `text/template` vs `html/template` escaping guidance (general Go community consensus, MEDIUM confidence, cross-checked against stdlib docs' own stated purpose for each package)
- DESIGN.md §2, §7, §9 — the project's own established pattern of rejecting C-binding/heavyweight dependencies for bypass-prone or out-of-scope code, applied here by extension to the XSD-validator and Traefik-module recommendations
- `.planning/threads/saml-sp-operator.md` — spike fixes K/L/N as concrete evidence for the "validate rendered XML against real shibd" testing recommendation

---
*Stack research for: Kubernetes operator implementation surface (Go/kubebuilder) beyond the existing scaffold*
*Researched: 2026-07-09*
