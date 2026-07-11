# Phase 1: Shared Render & Aggregation Package - Research

**Researched:** 2026-07-11
**Domain:** Pure Go config rendering (`encoding/xml` struct marshaling, `text/template`), deterministic collision resolution, config hashing — zero Kubernetes dependency
**Confidence:** HIGH (Go stdlib mechanics — verified against Go source/pkg.go.dev this session) / MEDIUM (test-harness/CI specifics — verified against public docs but not executed in this sandbox, no Go toolchain available here)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** `internal/render` takes **plain-Go input structs** (e.g. `render.SPConfig`, `render.AppBinding`) that the controllers populate — it does **not** import `api/v1alpha1` or `k8s.io/apimachinery`. The RequestMap's `(hostname, path)` data does not exist on the CRD spec at all (it is resolved from the HTTPRoute by the controller), and `createdAt`/UID live on `ObjectMeta`, not spec — so the package physically needs a synthesized input regardless of the dependency question.
- **D-02:** The "no k8s dependency" line is deliberate, not arbitrary purity: the render core is the shared seam between this operator (Traefik ForwardAuth attachment) and a planned standalone single-container deployment (nginx `auth_request` attachment). Keeping it k8s-free lets both consume it without a base-container merge that would be wrong. RENDER-08's per-attachment-model clear-list already encodes the two-consumer reality.
- **D-03:** `shibboleth2.xml` + `attribute-map.xml` are produced by `encoding/xml` struct marshaling (RENDER-01), using the **literal-`xmlns`-attribute approach**: declare `xmlns` and `xmlns:conf` as plain string attributes on the **root struct only** (`xml:"xmlns,attr"`, `xml:"xmlns:conf,attr"`), with every child element carrying a bare local-name tag (empty `Space`). A live Go 1.26 experiment confirmed this yields the target fixtures byte-for-byte; the namespace-aware `xml.Name{Space}` approach re-declares `xmlns` on every child (Go issue #9519, unresolved) and must not be used. Do not mix the two styles.
- **D-04:** `nginx.conf` is rendered via `text/template` (RENDER-07). The `<Host>` entries and `SHIBSP_SERVER_{NAME,SCHEME,PORT}`/absolute `handlerURL` self-URL derivation honor spike fixes M and N (explicit `scheme`+`port` on every `<Host>`, fully-qualified `handlerURL`) — the gate fails **open** otherwise.
- **D-05:** Injection safety (RENDER-10) rides on `encoding/xml`'s automatic escaping of element/attribute values, **plus an explicit `--` guard** on any CRD-derived string routed into an XML comment (spike fix K). The real failure mode is not FATAL XML: Go's marshaler *refuses to emit and returns an error* on any `--` inside a comment, so an unsanitized hostile/odd value would break config generation entirely. Strip at the input layer (`strings.ReplaceAll(v, "--", "-")`) before marshaling.
- **D-06:** Split **`Resolve(bindings) → Resolution{ Winners, Conflicts[] }`** (pure collision logic) from **rendering** (consumes winners). `Conflicts` carries `{ Winner, Loser (ns/name/UID), Hostname, Path }`. `Resolve` is exported and callable independently so the AppIntegration controller (APP-04) computes its **own** `Conflict` condition over its sibling list — never a cross-controller status write. This decomposition IS the "both controllers can never disagree" guarantee.
- **D-07:** Deterministic sort key: **`(priority desc, createdAt asc, UID asc)`** (RENDER-06). New `AppIntegration.spec.priority` (int32, higher wins, default 0) is consulted first, then oldest `createdAt`, then UID as final tiebreak. The UID tiebreak is load-bearing: `metav1.Time` is second-granular, so same-second creations tie on `createdAt` and MUST fall through to UID for determinism. Test the same-second case explicitly. `AppBinding` carries `Priority int32`; the CRD field lands when the AppIntegration controller maps CRD→`AppBinding` (Phase 3/5) or opportunistically earlier — planner's call.
- **D-08:** Config hash = one `sha256` over a stable, delimited concatenation of `{filename, bytes}` for **all rendered pod-config files: `shibboleth2.xml` + `nginx.conf` + `attribute-map.xml`** (RENDER-09). `attribute-map.xml` is `reloadChanges="false"` — shibd never hot-reloads it, so an attribute-only change must force a pod roll; excluding it from the hash is a silent correctness bug. `attribute-policy.xml`/`protocols.xml` are static image files in v1, not rendered, so out of hash scope. Deterministic render (D-07) is what makes the hash reorder-stable without a separate canonicalization pass.
- **D-09:** Two layers: (1) golden-file byte-compare of rendered artifacts against the spike fixtures; (2) a **build-tag/env-gated real-`shibd` container load test** that mounts the rendered `shibboleth2.xml` into a real shibd (the way `edge/testenv/docker-compose.yml` already does) and asserts it parses/loads — satisfying Phase 1 success criterion #1. Plain `go test` stays hermetic; the load test is the gated layer.
- **D-10:** CI feasibility confirmed: the SP image is a **public** GHCR package (`ghcr.io/jtickle/saml-sp-operator/shib-authenticator`) and `ubuntu-latest` has Docker, so the containerized load test is runnable in public-repo Actions. The load test image must stay publicly pullable (no employer/private infra).
- **D-11:** **Fail-safe rollout** is a hard project guarantee with three independent nets: (1) build-time — D-09's load test proves valid input → shibd-loadable output before anything ships; (2) admission-time — CEL rejects malformed specs (SEC-03, Phase 2); (3) runtime — readiness proves shibd loaded (SPI-03) + Deployment `RollingUpdate maxUnavailable: 0` (**SPI-07**, Phase 2). Phase 1 owns net (1).

### Claude's Discretion

- Exact Go struct layout, function/file names, and package sub-structure within `internal/render` (as long as `Resolve` is independently exported per D-06).
- Golden-fixture directory layout and the specific build-tag/env-var gating the load test.
- Whether the CRD `priority` field is added now vs at controller-wiring time (D-07).

### Deferred Ideas (OUT OF SCOPE)

- **Standalone single-container deployment tool** (nginx `auth_request` consumer of the render core) — future product surface, not v1. Phase 1 only preserves the option by keeping the package k8s-free; it does not build the tool.
- **`priority` as a documented v2 knob** — if oldest-wins + the field prove insufficient, richer precedence policy is a v2 concern.
- **`shibd` hot-reload for RequestMap edits** (OPS-03, v2) — v1 always rolls, gated by config-hash; no hot-reload path in the render package.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| RENDER-01 | Render `shibboleth2.xml` (ApplicationDefaults, `Sessions`, `MetadataProvider`, credentials) via `encoding/xml` struct marshaling | Self-closing-tag gap + literal-`xmlns` pattern + `xml.Header` prolog handling documented below (Code Examples, Common Pitfalls #1/#2) |
| RENDER-02 | Derive `SHIBSP_SERVER_{NAME,SCHEME,PORT}` + absolute `handlerURL` from each app's external URL | Scoped as a plain-Go *value* the package computes and returns (not an env-var render — that's Phase 2's job); see Architectural Responsibility Map |
| RENDER-03 | Render `attribute-map.xml` from `AppIntegration.attributes` | Same `encoding/xml` mechanics as RENDER-01; second literal-`xmlns`(+`xmlns:xsi`) root confirmed in the fixture |
| RENDER-04 | Aggregate bound `AppIntegration`s into one ordered RequestMap; most-specific path first, exact `<Host>` before `<HostRegex>` | Determinism requirement covered under RENDER-09/hash; ordering must never range a Go map when building output that feeds the hash |
| RENDER-05 | Every RequestMap `<Host>` carries explicit `scheme`+`port` even on default ports | Direct rendering rule; validated by a golden-file case with a default-port (443) `AppBinding` |
| RENDER-06 | Deterministic collision winner `(priority desc, createdAt asc, UID asc)`, loser excluded + flagged | `cmp.Or`/`slices.SortFunc` pattern (Code Examples); same-second-`createdAt` tiebreak test explicitly required |
| RENDER-07 | Render `nginx.conf` via `text/template` | No auto-escaping in `text/template` — confirmed pitfall from project PITFALLS.md #11, restated with concrete guard pattern below |
| RENDER-08 | Per-attachment-model header clear-list (Traefik enumerate-clear; nginx `Variable-*` glob) | **Scope clarification, not yet a rendered artifact** — see Common Pitfalls #6; this phase produces the pure-Go clear-list value, not a Middleware CRD or a `more_clear_input_headers` line in this repo's `nginx.conf` |
| RENDER-09 | `sha256` config hash over rendered bytes, stable/reorder-proof | Length-prefixed `{filename,bytes}` concatenation scheme (Code Examples) |
| RENDER-10 | Injection-safe against hostile CRD strings (`--`, `<`, `&`) | `encoding/xml` auto-escape confirmed; `--`-in-comment marshal error confirmed via Go source (Common Pitfalls #4) |
</phase_requirements>

## Summary

This phase is almost entirely a Go-stdlib exercise, and the single highest-leverage finding of this research session is that **`encoding/xml` never emits self-closing tags** — `Marshal`/`MarshalIndent` always write `<Foo></Foo>` for an empty element, never `<Foo/>`. This is confirmed directly from the Go 1.26 standard-library source (`writeStart`/`writeEnd` in `src/encoding/xml/marshal.go`), not inferred. The spike fixtures this phase must reproduce are full of self-closing leaf elements (`<Host .../>`, `<Handler .../>`, `<AttributeExtractor .../>`, `<CredentialResolver .../>`, etc.), so CONTEXT.md's D-03 claim of a "byte-for-byte" match via plain struct marshaling is **only achievable with an additional post-processing pass** that collapses `<Foo attrs...></Foo>` into `<Foo attrs.../>` after `MarshalIndent` runs. A third-party fork (`MarshalIndentShortForm`, still an open, unmerged Go proposal — issue #59710/#69273) does exactly this, but pulling in a stdlib-replacement fork for one formatting quirk is unnecessary; a ~15-line local regex/byte-scan helper is the right-sized fix and keeps the package dependency-free, consistent with the project's own established stdlib-only bias.

The second load-bearing finding is a **scope clarification, not a contradiction**: "byte-for-byte" against the *root* spike fixtures almost certainly cannot mean literally reproducing their hand-authored prose comments (the "SP_HOST_PLACEHOLDER" narration, the spike-fix explanations) or their manual attribute-column alignment (`attribute-map.xml`'s padded `name="email"     id="email"`) — no Go program should hardcode paragraphs of spike history into generated output, and `encoding/xml` cannot produce hand-aligned attribute columns at all. The practical reading — recommended here, flagged as an **Open Question** for the planner/user to confirm rather than assumed silently — is that byte-for-byte applies to the *semantic XML tree* (element/attribute set, ordering, self-closing form, `SP_HOST_PLACEHOLDER`→real-value substitution) with the render package's own purpose-built `testdata/` golden fixtures as the actual byte-compare target, while the root fixtures remain the human-readable reference for structure and the real authority for correctness is success criterion #1 (a real `shibd` parses and loads the output).

Everything else is comparatively low-risk and well-precedented: `text/template` for `nginx.conf` (no auto-escaping — must not interpolate raw CRD strings without validation, but this file's structure has few CRD-derived free-text fields), `crypto/sha256` for the config hash (stdlib, needs a length-prefixed concatenation scheme to avoid ambiguity), `cmp.Or`+`slices.SortFunc` (Go 1.21+ stdlib, already available at the pinned Go 1.26.0) for the priority/createdAt/UID collision sort, and `testcontainers/testcontainers-go` (well-established, actively maintained, Docker-only — no Kubernetes surface, so it doesn't violate D-01/D-02 even if imported inside `internal/render` behind a build tag) for the gated real-`shibd` load test. CI feasibility for D-10 is confirmed: GitHub-hosted `ubuntu-latest` runners have Docker preinstalled and can pull a **public** GHCR image with zero authentication — but the GHCR package must be manually flipped to public visibility (not automatic from the existing `build.yml`), and the existing workflow only builds on push to `spike`, so the load test should pin an explicit, immutable `sha`-tagged image rather than the floating `spike` branch tag.

**Primary recommendation:** Build `internal/render` as pure Go/stdlib only (`encoding/xml`, `text/template`, `crypto/sha256`, `sort`/`slices`/`cmp`), add one local ~15-line "collapse empty elements to self-closing" post-processing helper (the one piece of non-stdlib-obvious plumbing this phase needs), and add `github.com/testcontainers/testcontainers-go` as a single new dependency scoped to a build-tag-gated test file — nothing else. Resolve the "does byte-for-byte include the spike's prose comments" ambiguity explicitly before writing golden fixtures.

## Architectural Responsibility Map

This project's tiers don't match the standard web-app Browser/SSR/API/CDN/DB vocabulary (it's a Kubernetes operator generating config for a containerized SP, not a request-serving web app) — the table below substitutes this project's real tiers: **Pure Library** (`internal/render`, this phase, zero k8s dependency), **Controller** (Phase 2/3/4/5, imports the library, does the k8s I/O), and **Container Runtime** (the `shibd`/nginx pod the library's output feeds — out of scope, consumes the rendered bytes as a black box).

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `shibboleth2.xml` XML tree construction | Pure Library | — | RENDER-01; zero k8s dependency by D-01/D-02 |
| Self-URL **value** derivation (scheme/name/port/handlerURL) | Pure Library | Controller (Phase 2) | RENDER-02: the library computes the values and embeds `handlerURL` in the XML; the SPInstance controller (Phase 2) is who actually materializes `SHIBSP_SERVER_*` as container `env:` — the library must not assume it can render a Deployment |
| `attribute-map.xml` XML tree construction | Pure Library | — | RENDER-03 |
| RequestMap aggregation + host/path ordering | Pure Library | — | RENDER-04/RENDER-05; input assembly (resolving real HTTPRoutes) is Controller-tier (Phase 3/4), feeding plain `AppBinding` structs in |
| Collision resolution (`Resolve`) | Pure Library | Controller (Phase 3/4/5, calls it) | RENDER-06/D-06: both controllers call the *same* pure function — this IS the "can never disagree" guarantee |
| `nginx.conf` templating | Pure Library | — | RENDER-07 |
| Header clear-list *value* computation | Pure Library | Controller (Phase 5, emits Middleware) | RENDER-08: this phase computes the list/glob; actually emitting a Traefik `Middleware` CRD is Phase 5 (Controller tier), and the nginx-glob branch has no current-repo consumer at all (future standalone tool) |
| Config hash | Pure Library | Controller (Phase 2, stamps annotation) | RENDER-09: library computes the hash from bytes it produced; Phase 2 controller stamps it onto `spec.template.metadata.annotations` |
| Injection safety (escaping, `--` guard) | Pure Library | — | RENDER-10; defense-in-depth CEL validation is Phase 2 (Controller/admission tier), never a substitute for render-time escaping |
| Real `shibd` config parse/load proof | Container Runtime (via gated test) | Pure Library (test harness lives here) | D-09; the assertion target is the container, but the test that drives it lives in this phase's package |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `encoding/xml` (stdlib) | Go 1.26.0 (pinned in scaffold `go.mod`) [VERIFIED: repo `gsd/operator-scaffold` go.mod] | `shibboleth2.xml`/`attribute-map.xml` struct marshaling | RENDER-01/RENDER-03; gets escaping for free; already the project's locked decision (D-03) |
| `text/template` (stdlib) | Go 1.26.0 | `nginx.conf` rendering | RENDER-07/D-04; line-oriented, not tree-shaped — the right tool for this file |
| `crypto/sha256` + `encoding/hex` (stdlib) | Go 1.26.0 | Config hash (RENDER-09) | D-08; deterministic, no external dep needed |
| `cmp` + `slices` (stdlib, Go 1.21+) | Go 1.26.0 | Multi-key deterministic sort for `Resolve` (RENDER-06) | `cmp.Or(cmp.Compare(...), ...)` + `slices.SortFunc` is the current idiomatic stdlib pattern for exactly this "sort by field A, then B, then C" shape [CITED: brandur.org/fragments/cmp-or-multi-field, pkg.go.dev/slices] |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/testcontainers/testcontainers-go` | **v0.43.0**, published 2026-06-19 [CITED: pkg.go.dev/github.com/testcontainers/testcontainers-go?tab=versions — not independently re-verified via `go list -m` in this sandbox, no Go toolchain present; treat version number as ASSUMED-until-confirmed-at-plan-time] | Drives the gated real-`shibd` container load test (D-09) — starts the container, mounts rendered config, asserts startup | The build-tag/env-gated load-test file only; not imported by the hermetic `internal/render` package itself |
| `github.com/google/go-cmp` | v0.7.0 (already resolved transitively; promote `// indirect` → direct on next `go mod tidy`) [VERIFIED: repo `gsd/operator-scaffold` go.mod] | Struct-diff assertions in unit tests (e.g., comparing a `Resolution{}` value against expected) | Test-only; no new dependency actually needs adding, just promotion |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Local self-closing-tag collapse helper | `github.com/ttys3/go-xml` or `github.com/ECUST-XX/xml` (forks implementing `MarshalIndentShortForm`) | Both are unmerged-proposal forks of stdlib `encoding/xml` — replacing the entire `encoding/xml` import with a third-party fork for one formatting quirk is a much larger supply-chain surface than a 15-line local post-processing function; not recommended |
| `testcontainers-go` | Raw `os/exec` calling `docker run`/`docker compose up` + manual polling | testcontainers-go gives lifecycle management (cleanup, log-waiting via `wait.ForLog`), but a hand-rolled `os/exec` wrapper is a defensible zero-dependency alternative if Jeff prefers not to add a new module for this one gated test — flag as a discretion point for the planner |
| `cmp.Or`/`slices.SortFunc` | `sort.Slice` with a hand-written multi-field `less` function | Equivalent correctness; `cmp.Or` is more readable for a 3-key sort and is the current (Go 1.21+) idiom, but `sort.Slice` works identically on Go 1.26.0 if the team prefers the older, more universally-recognized pattern |

**Installation:**
```bash
go get github.com/testcontainers/testcontainers-go@v0.43.0   # verify exact current version at plan/execute time
go get github.com/google/go-cmp@v0.7.0                        # promote indirect -> direct
go mod tidy
```

**Version verification:** `go list -m -versions github.com/testcontainers/testcontainers-go` (or `go get -u` dry-run) should be re-run at plan/execute time — this session had no Go toolchain available to independently confirm via `go list`, so the v0.43.0 figure is CITED from pkg.go.dev, not tool-verified against the module proxy. Re-verify before locking the `go.mod` entry.

## Package Legitimacy Audit

Go modules don't have an npm/PyPI/crates-equivalent automated `package-legitimacy check` in this toolchain (the seam is ecosystem-scoped to npm/pypi/crates); the equivalent manual checks were performed against pkg.go.dev and GitHub.

| Package | Registry | Age | Adoption | Source Repo | Verdict | Disposition |
|---------|----------|-----|----------|-------------|---------|-------------|
| `github.com/testcontainers/testcontainers-go` | Go module proxy (proxy.golang.org) | Multi-year, active (latest release 2026-06-19) | 1,755+ importers per pkg.go.dev [CITED: pkg.go.dev] | `github.com/testcontainers/testcontainers-go` — official org, not a fork | OK | Approved |
| `github.com/google/go-cmp` | Go module proxy | Multi-year, Google-maintained | Already transitively present in this repo's `go.sum` | `github.com/google/go-cmp` — official Google org | OK | Approved (already vendored, just promote to direct) |

**Packages removed due to [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none. Both packages are widely-adopted, officially-namespaced modules; no `checkpoint:human-verify` gate needed for either, though re-confirming the exact `testcontainers-go` version against `go list -m` at execute time (no Go toolchain was available in this research sandbox) is still recommended hygiene.

## Architecture Patterns

### System Architecture Diagram

```
                    ┌───────────────────────────────────────────────────┐
                    │        internal/render (pure Go, no k8s)           │
                    │                                                     │
  SPConfig ────────▶│  1. Build XML struct tree (ApplicationDefaults,    │
  (entityID, IdP,   │     Sessions, MetadataProvider, CredentialResolver)│
   creds refs)      │             │                                      │
                    │             ▼                                      │
  []AppBinding ─────▶│  2. Resolve(bindings) → Resolution{Winners,       │
  (host, path,      │     Conflicts[]}  — pure sort/partition, no I/O    │
   priority,        │             │                                      │
   createdAt, UID)  │             ▼                                      │
                    │  3. Build RequestMap from Winners (exact-Host      │
                    │     before HostRegex, most-specific-path-first,    │
                    │     explicit scheme+port always)                   │
                    │             │                                      │
                    │             ▼                                      │
                    │  4. xml.MarshalIndent(tree, "", "    ")             │
                    │             │                                      │
                    │             ▼                                      │
                    │  5. collapseEmptyElements(bytes) → self-closing    │
                    │     tags (local helper — encoding/xml never does   │
                    │     this natively)                                 │
                    │             │                                      │
                    │             ▼                                      │
                    │  6. prepend xml.Header → shibboleth2.xml bytes     │
                    │                                                     │
                    │  (parallel path) attribute-map.xml: same steps     │
                    │  1/4/5/6 over a separate, smaller struct tree      │
                    │                                                     │
                    │  (parallel path) nginx.conf: text/template.Execute │
                    │  over a template + typed data struct                │
                    │             │                                      │
                    │             ▼                                      │
                    │  7. Hash({filename,bytes} for all 3 files) → sha256 │
                    └──────────────┬──────────────────────────────────────┘
                                   │  bytes + hash returned to caller
                                   ▼
      ┌─────────────────────────────────────────────────────────┐
      │  Hermetic go test (golden-file byte-compare, unit tests) │
      │  — always runs, no external deps                          │
      └─────────────────────────────────────────────────────────┘
                                   │
                                   ▼ (build-tag + env-var gated only)
      ┌─────────────────────────────────────────────────────────┐
      │  testcontainers-go starts ghcr.io/.../shib-authenticator  │
      │  mounts rendered shibboleth2.xml + attribute-map.xml,    │
      │  waits for shibd startup log, asserts no FATAL            │
      └─────────────────────────────────────────────────────────┘
```

### Recommended Project Structure
```
internal/render/
├── types.go            # SPConfig, AppBinding, Resolution, ClearListSpec — the plain-Go input/output shapes (D-01)
├── resolve.go           # Resolve(bindings []AppBinding) (Resolution, error) — pure collision logic (D-06)
├── resolve_test.go       # determinism test: same input, shuffled order, N runs, byte-identical Winners order; explicit same-second-createdAt tiebreak case
├── shibboleth2.go        # struct tree + Render() ([]byte, error) for shibboleth2.xml (RENDER-01/02/04/05)
├── attributemap.go       # struct tree + Render() ([]byte, error) for attribute-map.xml (RENDER-03)
├── nginxconf.go          # text/template + Render() ([]byte, error) for nginx.conf (RENDER-07)
├── clearlist.go          # RENDER-08: pure value computation, Traefik-enumerate vs nginx-glob
├── xmlformat.go          # collapseEmptyElements() helper + xml.Header prepend — the one non-obvious plumbing piece
├── confighash.go         # Hash(files []ConfigFile) string — RENDER-09/D-08
├── inject_test.go        # RENDER-10 adversarial fuzz: <, &, --, ]]>  through every string field
├── shibdload_test.go      # //go:build shibdload — testcontainers-go real-shibd load test (D-09/D-10)
└── testdata/
    ├── golden/            # this package's own byte-compare targets (see Open Question on "byte-for-byte" scope)
    └── fixtures/           # sample SPConfig/[]AppBinding literals used across tests
```

### Structure Rationale

- **One file per rendered artifact**, each exposing a single `Render(...) ([]byte, error)` entry point — keeps golden-file tests trivially one-to-one with source files, and matches this project's own `internal/render/` sketch already present in ARCHITECTURE.md research.
- **`xmlformat.go` isolates the one piece of non-stdlib-obvious behavior** (self-closing tag collapse) so it's easy to find, unit-test in isolation (feed it known `<Foo></Foo>` / `<Foo>text</Foo>` cases, assert only the empty one collapses), and reuse across both `shibboleth2.go` and `attributemap.go`.
- **`shibdload_test.go` behind a build tag** keeps `go build ./...`/`go vet ./...`/plain `go test ./...` fully hermetic (no Docker requirement) while still shipping the mandatory real-`shibd` proof as an opt-in CI job.

### Pattern 1: Self-closing tag reproduction via post-processing (the load-bearing new finding)

**What:** `encoding/xml`'s `Marshal`/`MarshalIndent` unconditionally write a start tag and a matching end tag for every element — there is no code path in the stdlib encoder that ever emits `<Foo/>`, confirmed directly from `src/encoding/xml/marshal.go`'s `writeStart`/`writeEnd` (the encoder always writes the literal `<`, `/`, name, `>` sequence for the closing tag; there is no branch that instead writes `/>` on the opening tag). The `MarshalIndentShortForm` function some search results reference does **not** exist in the standard library — it is an unmerged, years-open proposal (golang/go#59710, golang/go#69273) implemented only in third-party forks. [VERIFIED: golang/go source, `src/encoding/xml/marshal.go`, fetched directly this session]

**When to use:** Every leaf element in the spike fixtures that has attributes but no child content — `<Host .../>`, `<Handler .../>`, `<SSO>...</SSO>` (NOT self-closing, has chardata — contrast case), `<AttributeExtractor .../>`, `<AttributeResolver .../>`, `<AttributeFilter .../>`, `<CredentialResolver .../>`, `<SecurityPolicyProvider .../>`, `<ProtocolProvider .../>`, every `<Attribute .../>` in `attribute-map.xml`.

**Example:**
```go
// xmlformat.go — the one piece of non-obvious plumbing this phase needs.
// encoding/xml never self-closes; collapse "<Foo attrs...></Foo>" -> "<Foo attrs.../>"
// after MarshalIndent runs. Anchored on a closing-tag-name backreference so it only
// ever touches genuinely-empty elements (chardata-bearing elements like <SSO>...</SSO>
// never match — there's content between the tags).
var emptyElementRE = regexp.MustCompile(`(?s)<([A-Za-z][\w:.-]*)((?:\s+[\w:.-]+="[^"]*")*)\s*></([A-Za-z][\w:.-]*)>`)

func collapseEmptyElements(b []byte) []byte {
	return emptyElementRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := emptyElementRE.FindSubmatch(m)
		name, attrs, closeName := sub[1], sub[2], sub[3]
		if string(name) != string(closeName) {
			return m // defensive: malformed match, leave untouched
		}
		return []byte("<" + string(name) + string(attrs) + "/>")
	})
}

func Render(cfg SPConfig) ([]byte, error) {
	tree := buildShibboleth2Tree(cfg) // pure struct construction, no I/O
	body, err := xml.MarshalIndent(tree, "", "    ")
	if err != nil {
		return nil, err
	}
	body = collapseEmptyElements(body)
	return append([]byte(xml.Header), body...), nil // xml.Header = `<?xml version="1.0" encoding="UTF-8"?>` + newline
}
```

**Trade-offs:** A regex-based post-process is a byte-level hack, not type-safe — but it's ~15 lines, has zero new dependencies, is trivially unit-testable in isolation (feed it fixed strings, assert exact output), and is the same technique the unmerged third-party forks use internally. The alternative (hand-writing `MarshalXML` per struct type using low-level `Encoder.EncodeToken`) is far more code for the same result, since there's no `EmptyElement` token type in the stdlib to call even at that level (also unmerged, golang/go#69273).

### Pattern 2: Deterministic multi-key sort for collision resolution

**What:** Go 1.21+ added `cmp.Compare`/`cmp.Or` in the stdlib `cmp` package specifically for chained multi-field comparators, paired with `slices.SortFunc`. [CITED: brandur.org/fragments/cmp-or-multi-field; pkg.go.dev/slices — both confirm the pattern]

**When to use:** `Resolve`'s winner-selection sort (RENDER-06/D-07): priority desc, createdAt asc, UID asc.

**Example:**
```go
// Source: pattern verified against pkg.go.dev/slices + community usage (brandur.org)
import ("cmp"; "slices")

type AppBinding struct {
	Namespace, Name string
	UID             string // types.UID is a string underneath; keep the render package k8s-free per D-01/D-02
	Hostname, Path  string
	Priority        int32
	CreatedAtUnix   int64 // pre-truncated to whole seconds by the caller — see Common Pitfalls #3
}

func rankOrder(bindings []AppBinding) []AppBinding {
	out := slices.Clone(bindings)
	slices.SortFunc(out, func(a, b AppBinding) int {
		return cmp.Or(
			cmp.Compare(b.Priority, a.Priority),       // desc: b before a when b.Priority > a.Priority
			cmp.Compare(a.CreatedAtUnix, b.CreatedAtUnix), // asc
			cmp.Compare(a.UID, b.UID),                 // asc, final tiebreak
		)
	})
	return out
}
```

**Trade-offs:** `slices.SortFunc` is **not** guaranteed stable (`slices.SortStableFunc` is, if that matters) — but since the UID tiebreak makes the comparator a strict total order (no two distinct bindings can ever compare equal, because UIDs are unique), stability is a non-issue here. Note this explicitly in the implementation so a future reader doesn't "fix" it into `SortStableFunc` under a mistaken belief it's needed.

### Pattern 3: Length-prefixed config hash concatenation

**What:** A hash over `{filename, bytes}` pairs needs an unambiguous encoding, or two different `(filename, content)` splits can collide (e.g., `("ab", "c")` vs `("a", "bc")` naively concatenated both hash to `"abc"`).

**Example:**
```go
// confighash.go
type ConfigFile struct {
	Name  string
	Bytes []byte
}

// Hash is deterministic iff files is provided in a stable, caller-fixed order.
// Recommend a fixed explicit order (shibboleth2.xml, nginx.conf, attribute-map.xml)
// rather than sorting by name here — makes the hash's input order self-documenting
// at the call site and avoids a second sort dependency inside this function.
func Hash(files []ConfigFile) string {
	h := sha256.New()
	for _, f := range files {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(f.Name)))
		h.Write(lenBuf[:])
		h.Write([]byte(f.Name))
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(f.Bytes)))
		h.Write(lenBuf[:])
		h.Write(f.Bytes)
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

**Trade-offs:** Length-prefixing is the standard fix for this class of ambiguity — cheap, no external dependency, and self-documenting. A simpler `strings.Join`-with-delimiter scheme is also defensible if the delimiter is guaranteed never to appear in a filename (true here — filenames are hardcoded constants, not CRD-derived), but length-prefixing costs nothing extra and removes the "guaranteed never" assumption entirely.

### Anti-Patterns to Avoid

- **Ranging over a Go map anywhere in the render path:** Go intentionally randomizes map iteration order — any `for k, v := range someMap` feeding XML/template output produces a byte-different-but-semantically-identical render on every process restart, defeating RENDER-09's hash stability. Already documented in project PITFALLS.md #10; restated here because it's a Phase 1 acceptance criterion (ROADMAP success criterion #4), not just a general caution.
- **Trusting `xml.MarshalIndentShortForm` or any third-party self-closing-tag fork as "the fix":** it does not exist in the standard library at the Go 1.26.0 pin this project uses; do not write code (or accept a plan) that assumes stdlib self-closes.
- **Rendering the `SHIBSP_SERVER_*` env vars as part of this package's output:** RENDER-02 is a self-URL *value* derivation, not a Deployment env-var render — `internal/render` has no `corev1.EnvVar` type available to it (and must not, per D-01/D-02). Return the scheme/name/port as plain fields on the render input/output; Phase 2's SPInstance controller is who turns them into `env:`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| XML escaping of attribute/element values | A custom XML-escaping function for CRD strings | `encoding/xml` struct marshaling (already the D-03 decision) | Escaping is automatic and correct by construction for every string field that goes through a normal struct tag |
| Self-closing tag support | A hand-rolled full XML writer, or vendoring a fork of `encoding/xml` | The ~15-line `collapseEmptyElements` post-process (Pattern 1) | Smallest possible fix for the smallest possible gap; keeps `encoding/xml` as the source of truth for correctness (escaping, structure) and only patches formatting |
| Multi-field deterministic sort | A hand-written `less(a, b) bool` with nested `if`/`else` branches | `cmp.Or` + `slices.SortFunc` (Pattern 2) | Equivalent to a hand-written `less`, but the chained-`cmp.Compare` form is harder to get subtly wrong (e.g., forgetting to negate the priority comparison, or short-circuiting `&&` incorrectly across levels) |
| Container lifecycle for the load test | A hand-rolled `os/exec.Command("docker", "run", ...)` + polling loop | `testcontainers-go` (or, if Jeff prefers zero new deps, a deliberately-scoped `os/exec` wrapper — flagged as a real alternative above, not just a throwaway) | `wait.ForLog`-style startup detection and guaranteed cleanup (even on test failure/panic) are exactly the kind of "looks simple, has subtle timing bugs" problem a maintained library solves once |

**Key insight:** The only genuinely new/non-obvious problem this phase introduces is the self-closing-tag gap — everything else (escaping, sorting, hashing, container orchestration) has a stdlib or well-established-library answer. Don't let the self-closing-tag surprise motivate reaching for a bigger hammer (a full third-party XML library) than the problem needs.

## Common Pitfalls

### Pitfall 1 [NEW — this session's primary finding]: `encoding/xml` never self-closes empty elements

**What goes wrong:** A struct with only attributes and no chardata field marshals to `<Foo attr="x"></Foo>`, never `<Foo attr="x"/>`. If the plan assumes `xml.MarshalIndent` alone reproduces the spike fixtures byte-for-byte (as CONTEXT.md's D-03 narrative implies), the golden-file tests will fail on every self-closing leaf element in `shibboleth2.xml`/`attribute-map.xml` — which is most of them.

**Why it happens:** This is a genuine, long-standing stdlib gap (open since at least Go issue #21399), not a misconfiguration. There is no `Marshal` option, `Encoder` method, or struct tag that changes this behavior in the standard library as of Go 1.26.0.

**How to avoid:** Add the `collapseEmptyElements` post-processing step (Pattern 1) as a mandatory part of both `shibboleth2.go`'s and `attributemap.go`'s `Render()` functions, and unit-test it in isolation with both a positive case (`<Foo x="1"></Foo>` → `<Foo x="1"/>`) and a negative case (`<Foo>text</Foo>` must NOT collapse — content with real chardata must survive untouched).

**Warning signs:** Golden-file diff shows every self-closing element in the fixture as a mismatch with the rendered output's paired-tag form; `shibd`'s own XML parser will actually accept BOTH forms (self-closing and paired empty tags are semantically identical XML) — so this bug does **not** break RENDER-01's shibd-load success criterion, it only breaks a literal byte-diff against the human-authored fixture. Don't let a passing shibd-load test mask a failing/skipped byte-compare test.

**Phase to address:** This phase (config-rendering) — the `collapseEmptyElements` helper must exist before any golden-file test can pass.

---

### Pitfall 2: `encoding/xml` never emits the `<?xml ...?>` declaration

**What goes wrong:** `Marshal`/`MarshalIndent` output starts directly with the root element — no `<?xml version="1.0" encoding="UTF-8"?>` prolog. Every fixture (`shibboleth2.xml`, `attribute-map.xml`) starts with this line.

**Why it happens:** Documented stdlib behavior — the `xml.Header` constant exists specifically because the package deliberately does not add it automatically (so callers who are appending to a larger document, or who don't want the declaration, aren't forced to strip it). [CITED: pkg.go.dev/encoding/xml, `xml.Header` constant]

**How to avoid:** `append([]byte(xml.Header), marshaledBody...)` — `xml.Header`'s value already includes the trailing newline matching the fixture format.

**Warning signs:** Golden-file diff shows the rendered output missing its first line entirely.

**Phase to address:** This phase.

---

### Pitfall 3: `metav1.Time` second-granularity breaks naive `createdAt` comparisons if truncation isn't explicit

**What goes wrong:** If the render package accepts a raw `time.Time` with sub-second precision from a test fixture (but real `ObjectMeta.CreationTimestamp` values are always whole-second, since Kubernetes' `metav1.Time` JSON-marshals to RFC3339 second precision), a unit test that constructs two bindings 500ms apart will pass the sort test even though real-world bindings created 500ms apart would tie at the same second and require the UID tiebreak — silently under-testing the exact scenario D-07 calls out as load-bearing.

**Why it happens:** `time.Time` in Go has nanosecond precision by default; nothing forces a test fixture to mimic Kubernetes' RFC3339-second truncation unless the author does it deliberately.

**How to avoid:** The `Resolve` input type should either (a) take an already-second-truncated field (e.g., `CreatedAtUnix int64`, forcing the caller/controller to truncate at the k8s boundary — recommended, keeps `internal/render` from needing to know about `metav1.Time` at all, consistent with D-01), or (b) explicitly document that any `time.Time` field must be `.Truncate(time.Second)`-ed by the caller. Either way, the same-second-tiebreak test (`resolve_test.go`) MUST construct two bindings with an identical truncated timestamp and differing UIDs, and assert the UID-lower one wins.

**Phase to address:** This phase — this is exactly the test D-07 explicitly requires ("Test the same-second case explicitly").

---

### Pitfall 4: The `--`-in-comment guard has two different enforcement paths — know which one applies

**What goes wrong:** Go's `encoding/xml` has two distinct code paths for comments: (a) the low-level `Encoder.EncodeToken(xml.Comment(...))` API only rejects a comment containing the literal `-->` sequence, NOT a bare `--`; (b) the struct-field path (a field tagged `xml:",comment"`) rejects **any** `--` and returns the error `xml: comments must not contain "--"`. [VERIFIED: golang/go source, `src/encoding/xml/marshal.go`, fetched directly this session] If the render package's implementation ever switches from struct-tag comments to manual `EncodeToken` calls (e.g., to get finer control over placement), the stricter guarantee silently weakens.

**Why it happens:** These are genuinely two different code paths in the stdlib with different validation strength; it's easy to assume "comments are always `--`-checked" without realizing which API surface provides that guarantee.

**How to avoid:** If this phase renders any operator-generated comments at all (e.g., a "rendered by saml-sp-operator, do not edit" marker, or per Pitfall/D-05, any CRD-derived string routed into a comment), use the struct-tag `,comment` path — it's the one that actually enforces the `--`-rejection D-05 relies on. Also keep the explicit `strings.ReplaceAll(v, "--", "-")` input-layer strip D-05 already specifies as defense-in-depth, since relying solely on the marshaler returning an error means a single hostile field aborts the *entire* render (a real availability concern per DESIGN's fail-safe framing) rather than being silently sanitized.

**Phase to address:** This phase.

---

### Pitfall 5 [scope ambiguity, not a bug]: "Byte-for-byte" against the root fixtures likely does not include their prose comments or manual alignment

**What goes wrong:** The root `shibboleth2.xml` fixture's first 15 lines are hand-authored spike narration (`SP_HOST_PLACEHOLDER` instructions, an explanation of what to do "if shibd refuses to start"), and every `<Sessions>`/`<RequestMapper>` block is preceded by paragraph-length educational comments explaining spike fixes M/N/K. The `attribute-map.xml` fixture uses hand-aligned attribute columns (`name="email"     id="email"` — extra spaces for visual alignment across sibling `<Attribute>` elements) that `encoding/xml.MarshalIndent` has no mechanism to reproduce (it has no concept of "align this attribute value with the sibling three lines down"). Treating "byte-for-byte" as covering these elements will produce an impossible or absurd implementation target (a Go program hardcoding paragraphs of spike-fix commentary as generated output).

**Why it happens:** CONTEXT.md's instruction to reproduce "byte-for-byte" the fixtures at repo root doesn't explicitly scope out human-authored commentary/formatting, and D-03's "confirmed byte-for-byte" claim doesn't specify which subset of bytes.

**How to avoid:** Treat the *semantic XML tree* (element/attribute set + values + nesting + self-closing form, with `SP_HOST_PLACEHOLDER` substituted for a real test host) as the byte-for-byte target, and build the render package's own `testdata/golden/` fixtures (comment-free or minimally-commented, machine-formatted) as the actual byte-compare target for `resolve_test.go`/unit tests — using the root fixtures as the human-readable *structural* reference, not the literal diff target. **Flag this explicitly to the user/planner as an assumption to confirm**, not a silent resolution — see Open Questions below.

**Phase to address:** This phase, before golden fixtures are written (affects `testdata/` layout).

---

### Pitfall 6 [scope clarification]: RENDER-08's nginx-glob branch has no current-repo consumer

**What goes wrong:** A planner reading RENDER-08 ("edge header hygiene clear-list renders correctly for both the Traefik... and nginx... attachment models") might expect this phase to modify the repo-root `nginx.conf` fixture to add a `more_clear_input_headers 'Variable-*'` line, or to golden-file-test against it. The root `nginx.conf`'s role in **this** project is a pure FastCGI→HTTP adapter for the Traefik-ForwardAuth model — it has no `ngx_http_shibboleth`/`headers_more` modules loaded and no clear-list directive at all. The `Variable-*` wildcard-glob clear-list is for the **deferred, future, non-k8s standalone tool** (per CONTEXT.md's Deferred Ideas and the project's `shared-render-core-intent` memory), which doesn't exist as a consumer in this repo yet.

**Why it happens:** RENDER-08's wording names both attachment models symmetrically, but only one of them (Traefik enumerate-clear) has a real Phase-5 consumer in the current roadmap; the other is architecturally anticipated but not yet built.

**How to avoid:** Scope RENDER-08 in this phase as a **pure-Go value computation only** — e.g., `render.ClearList(model AttachmentModel) ClearListSpec` returning either an explicit header-name list (Traefik) or a glob pattern string (nginx) — unit-tested for correctness of the returned value, with **no** golden-file comparison against `nginx.conf` (there is nothing in that file to compare against) and no expectation that this phase touches the repo-root `nginx.conf` fixture's content at all.

**Phase to address:** This phase (the value computation); actual Traefik Middleware emission is Phase 5; the nginx-glob consumer doesn't exist yet.

---

### Pitfall 7 [operational, not code]: The reference load-test harness file is cross-contaminated with an unrelated product's content

**What goes wrong:** CONTEXT.md and the phase brief point to `edge/testenv/docker-compose.yml` as "the load-test harness reference... reuse its shape." Reading that file (branch `spike`, single commit `84a5f74`) shows its actual content is a **different Tickle Technologies product's** testbed — headed `# an unrelated product's delegated-auth testbed`, referencing its backend server, port comments about avoiding clashes with "anything on 8080," and a `simplesamlphp` IdP rather than mocksaml — not this project's SAML SP operator at all. The adjacent `edge/Dockerfile` and `edge/nginx.validate.conf` are similarly flavored for that other product (`ngx_http_shibboleth`/`headers_more` modules for that product's reference nginx deployment). This is very likely a copy-paste artifact from a working directory shared across Jeff's projects, not intentional shared infrastructure for this repo.

**Why it happens:** Unclear from the available history — the whole `edge/` directory landed in one spike commit; there's no earlier, uncontaminated version in this repo's history to fall back to.

**How to avoid:** Do **not** reuse `edge/testenv/docker-compose.yml` verbatim as instructed literally — reuse only its *structural shape* (bind-mount pattern: `shibboleth2.xml`/`attribute-map.xml`/`attribute-policy.xml`/`nginx.conf`/`sp-credentials/` mounted read-only into the SP container) and write a fresh, project-native test fixture (or construct the equivalent mounts programmatically via `testcontainers-go`'s `ContainerRequest.Files`) using **this repo's own root-level `shibboleth2.xml`/`attribute-map.xml`/`nginx.conf`** as the reference shape and the render package's own rendered output as the actual mounted content. Given this is a **public** repo (per project constraints), also worth a quick check that nothing product-specific in `edge/` needs redacting — it appears to be generic reference-deployment content, not credentials/infra-identifying, but flag for a human glance since it's out of this phase's direct scope.

**Phase to address:** This phase, when building the D-09 load-test harness — don't let this surprise block planning, but don't let the planner assume the existing file is a drop-in.

---

### Pitfall 8 [CI operational]: GHCR image publish is branch-gated and visibility is not automatic

**What goes wrong:** The existing `.github/workflows/build.yml` only triggers on push to the `spike` branch (`on: push: branches: [spike]`) and pushes tags `spike` (floating) and a short-sha tag (immutable) to `ghcr.io/jtickle/saml-sp-operator/shib-authenticator`. Two separate risks: (1) a GHCR package's default visibility is **private** even when the repo is public — pulling it anonymously in CI requires someone to have manually flipped the package to public in GHCR's own settings (not something `build.yml` does), and this is a one-way, irreversible switch once done; (2) if the load test pins the floating `spike` tag and someone later pushes an unrelated change to the `Dockerfile`/`supervisord.conf` on that branch, the image the load test pulls can silently change out from under Phase 1's tests.

**Why it happens:** GHCR's private-by-default behavior is a platform default, not something the workflow author necessarily configured deliberately; the `spike`-branch-only trigger predates this phase's need to run the load test from a phase branch off `main`.

**How to avoid:** (1) Confirm (or have Jeff confirm) the `shib-authenticator` GHCR package is set to Public in the repo's Packages settings before relying on anonymous pulls in CI — this is a one-time manual gate outside this phase's code. (2) Pin the load test to an explicit, immutable **short-sha tag** captured at a known-good point (documented as a constant in the test file, e.g. `ghcr.io/jtickle/saml-sp-operator/shib-authenticator:<sha>`), not the floating `spike`/`main` tag — Phase 1 doesn't touch the Docker image at all, so an existing sha-tagged image is sufficient and won't drift.

**Phase to address:** This phase, as a plan-time decision (which tag to pin) plus a documented dependency on the manual GHCR visibility toggle (outside code, needs a `checkpoint:human-verify`-style callout in the plan).

## Code Examples

### Building the RequestMap `<Host>` with mandatory scheme+port (RENDER-05)

```go
// Source: derived from the spike fixture's own inline documentation
// (shibboleth2.xml lines 24-35) plus RENDER-05's explicit wording — no
// special-casing for "port 443/80 is safe to omit." Every Host always
// carries scheme+port, full stop.
type hostElement struct {
	XMLName        xml.Name `xml:"Host"`
	Name           string   `xml:"name,attr"`
	Scheme         string   `xml:"scheme,attr"`
	Port           int      `xml:"port,attr"`
	AuthType       string   `xml:"authType,attr,omitempty"`
	RequireSession string   `xml:"requireSession,attr,omitempty"`
}

func buildHost(b AppBinding) hostElement {
	return hostElement{
		Name:           b.Hostname,
		Scheme:         b.Scheme, // "https", derived from the app's external URL — never defaulted/inferred
		Port:           b.Port,   // e.g. 443 — ALWAYS emitted, even though it's the scheme default
		AuthType:       "shibboleth",
		RequireSession: "true",
	}
}
```

### `text/template` guard against unescaped CRD strings in `nginx.conf` (RENDER-07/RENDER-10)

```go
// Source: pattern derived from project PITFALLS.md #11's documented gap
// (text/template performs NO auto-escaping) — the guard is applying an
// explicit allowlist/validation at the Go layer BEFORE the value reaches
// the template, not relying on any template-side escaping mechanism
// (there isn't one for text/template).
var validHostnameRE = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

func validateHostname(h string) error {
	if !validHostnameRE.MatchString(h) {
		return fmt.Errorf("render: hostname %q contains characters invalid for nginx directive context", h)
	}
	return nil
}
// Call validateHostname on every CRD-derived string BEFORE passing the
// template data struct to tmpl.Execute — reject at render time, don't
// attempt to escape nginx directive syntax after the fact.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| `sort.Slice` with hand-written multi-field `less` closures | `cmp.Or(cmp.Compare(...), ...)` + `slices.SortFunc`/`slices.SortStableFunc` | Go 1.21 (stdlib `cmp`/`slices` packages) | Both remain fully correct on Go 1.26.0; `cmp.Or` is simply the more idiomatic/readable form for a 3-key comparator as of the currently-pinned Go version — not a hard requirement, a style recommendation |
| Hoping `encoding/xml` gains self-closing-tag support | Still doesn't exist in stdlib as of Go 1.26.0; remains an open, years-old proposal (#59710/#69273) | No change — flagged here specifically because it's easy to assume a modern Go version fixed this | Don't plan around a future stdlib fix landing during this phase's execution window |

**Deprecated/outdated:** None specific to this phase's stack — `encoding/xml`, `text/template`, and `crypto/sha256` are all stable, unchanging stdlib APIs with no deprecation risk.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `testcontainers-go` current version is v0.43.0 (published 2026-06-19) | Standard Stack | Low — cosmetic `go.mod` pin mismatch, caught immediately by `go get`/`go mod tidy` at execute time; this session had no Go toolchain to independently confirm via `go list -m` |
| A2 | "Byte-for-byte" (D-03/CONTEXT.md) scopes to the semantic XML tree, not the root fixtures' hand-authored prose comments or manual attribute-column alignment | Common Pitfalls #5, Summary | Medium — if the actual intent WAS literal byte-for-byte including commentary, the planner needs to scope tasks very differently (a much larger, arguably-nonsensical "reproduce this prose" requirement); recommend confirming with Jeff before writing golden fixtures, since this wasn't explicitly resolved in the CONTEXT.md discussion |
| A3 | The `edge/testenv/docker-compose.yml` branded for that other product content is an unintentional copy-paste artifact, not deliberate shared infrastructure | Common Pitfalls #7 | Low-Medium — if it's actually intentional (e.g., a real cross-project shared testbed), the planner should ask rather than silently write a parallel file; either way the recommendation (don't copy verbatim, build a project-native fixture) holds |
| A4 | The `shib-authenticator` GHCR package's visibility has not yet been explicitly set to Public | Common Pitfalls #8 | Medium — if it's actually already public, this is a no-op confirmation; if it's still private, the load test silently fails to pull in CI until someone flips the setting — worth an explicit plan-time check rather than discovering it at CI-execution time |

**If this table is empty:** N/A — see entries above.

## Open Questions

1. **Does "byte-for-byte" reproduction of the golden fixtures include the spike's hand-authored prose comments and manual attribute-column alignment, or only the semantic XML tree?**
   - What we know: `encoding/xml` cannot reproduce either (no comment-paragraph-generation logic would make sense, and there's no mechanism for column-aligned attribute output).
   - What's unclear: whether CONTEXT.md's D-03 "confirmed byte-for-byte" claim was scoped to a stripped/minimal test fixture (most likely) or literally the root files as they exist today.
   - Recommendation: Confirm with Jeff before building `testdata/golden/` fixtures; default assumption (recommended) is semantic-tree-only, with the render package's own purpose-built golden files as the actual test target and success criterion #1 (real shibd load) as the authoritative correctness gate, per the ROADMAP's own explicit "not just a golden-file text-compare" framing.

2. **Is the `shib-authenticator` GHCR package already public?**
   - What we know: the current `build.yml` pushes to GHCR but does not (and cannot, from within the workflow alone) set package visibility — that's a one-time manual GitHub UI action, and it's a one-way toggle once flipped.
   - What's unclear: whether Jeff already did this during the spike, since the load test's CI feasibility (D-10) depends on it entirely.
   - Recommendation: Verify (`docker logout ghcr.io && docker pull ghcr.io/jtickle/saml-sp-operator/shib-authenticator:spike` from a machine with no stored GHCR credentials) before or during planning; if still private, flip it as a `checkpoint:human-verify` task early in the plan, since every other D-09/D-10 task depends on it.

3. **Should `internal/render`'s load-test file use `testcontainers-go` or a hand-rolled `os/exec` wrapper?**
   - What we know: both are viable; `testcontainers-go` is the more common idiom and handles cleanup/log-waiting robustly, but is a genuinely new dependency for a project that has otherwise stayed stdlib-only in Phase 1's other choices.
   - What's unclear: whether Jeff has a preference given the project's general "avoid unnecessary dependencies" pattern (STACK.md's Traefik-types decision, sprig rejection, etc.).
   - Recommendation: Default to `testcontainers-go` (this research's recommendation, given the cleanup/reliability properties matter for a CI-gated test), but flag as a legitimate discretion point, not a locked call — CONTEXT.md already delegates "the specific build-tag/env-var gating the load test" to Claude's discretion, and this is squarely part of that.

## Environment Availability

| Dependency | Required By | Available (this research sandbox) | Version | Fallback |
|------------|------------|:---:|---------|----------|
| Go toolchain (1.26.0 pinned) | All of Phase 1 (compiling/testing the package) | ✗ (not installed in this research sandbox — confirm in the actual execution environment) | — | None — Go is a hard requirement; this is a sandbox limitation of the research session, not a finding about Jeff's actual dev/CI environment |
| Docker | D-09/D-10 gated load test (both local execution and CI) | ✓ (Docker 29.6.1 present in this research sandbox) | 29.6.1 | — |
| GitHub Actions `ubuntu-latest` runner Docker support | D-10 CI feasibility | N/A (not directly probed — verified via public documentation instead) | — [CITED: GitHub Docs/community discussions confirm Docker is preinstalled on `ubuntu-latest` and public GHCR images pull anonymously with no login step] | — |
| GHCR package public visibility for `shib-authenticator` | D-10 CI feasibility | Unconfirmed — see Open Question 2 | — | Flip visibility to Public in GHCR package settings (one-time, manual, irreversible) |

**Missing dependencies with no fallback:**
- Go 1.26.0 toolchain in the actual execution environment — this research session had none installed and could not run `go build`/`go test` to empirically verify any of the XML-marshaling claims above; all Go-source-level findings were verified by reading `golang/go`'s actual source on GitHub rather than by executing code. Recommend the planner or a `checkpoint:human-verify` step run a small standalone reproduction (`xml.MarshalIndent` on a minimal empty-element struct) at plan/execute time to confirm the self-closing-tag finding empirically in the real environment before committing to the `collapseEmptyElements` implementation shape.

**Missing dependencies with fallback:**
- GHCR public visibility (see Open Question 2) — fallback is the one-time manual toggle, not a code change.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (`go test`) — no third-party test framework needed for this pure-Go package; the project's existing `ginkgo`/`gomega` (v2.27.4/v1.39.0, per scaffold `go.mod`) are for controller-runtime/envtest suites in later phases, not required here |
| Config file | none — `go test ./internal/render/...` needs no config; the build-tag-gated load test needs no separate config file either, just the `-tags` flag |
| Quick run command | `go test ./internal/render/...` |
| Full suite command | `go test -tags shibdload ./internal/render/... -run TestShibdLoad -v` (requires Docker + network access to pull the pinned GHCR image) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| RENDER-01 | shibboleth2.xml renders correct structure | golden-file (unit) | `go test ./internal/render/... -run TestRenderShibboleth2` | ❌ Wave 0 |
| RENDER-01 | rendered shibboleth2.xml is loadable by real shibd | gated integration | `go test -tags shibdload ./internal/render/... -run TestShibdLoad` | ❌ Wave 0 |
| RENDER-02 | self-URL values (scheme/name/port/handlerURL) consistent | unit | `go test ./internal/render/... -run TestSelfURLConsistency` | ❌ Wave 0 |
| RENDER-03 | attribute-map.xml renders correct structure | golden-file (unit) | `go test ./internal/render/... -run TestRenderAttributeMap` | ❌ Wave 0 |
| RENDER-04 | RequestMap ordering: exact-Host before HostRegex, most-specific-path-first | unit | `go test ./internal/render/... -run TestRequestMapOrdering` | ❌ Wave 0 |
| RENDER-05 | every Host carries explicit scheme+port, incl. default ports | unit (negative-case included: default-port 443 case must still show explicit attrs) | `go test ./internal/render/... -run TestHostSchemePort` | ❌ Wave 0 |
| RENDER-06 | deterministic winner (priority desc, createdAt asc, UID asc); same-second tiebreak | property/determinism | `go test ./internal/render/... -run TestResolveDeterminism` | ❌ Wave 0 |
| RENDER-07 | nginx.conf renders correctly | golden-file (unit) | `go test ./internal/render/... -run TestRenderNginxConf` | ❌ Wave 0 |
| RENDER-08 | clear-list value correctness per model | unit | `go test ./internal/render/... -run TestClearList` | ❌ Wave 0 |
| RENDER-09 | sha256 hash stable across map-order reshuffles, changes iff content changes, includes attribute-map.xml | property (determinism + change-sensitivity) | `go test ./internal/render/... -run TestConfigHash` | ❌ Wave 0 |
| RENDER-10 | hostile strings (`--`, `<`, `&`, `]]>`) never produce invalid/FATAL-ing XML | adversarial fuzz/property | `go test ./internal/render/... -run TestInjectionSafety` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/render/...` (hermetic, no Docker — fast enough to run on every commit)
- **Per wave merge:** `go test -tags shibdload ./internal/render/... -v` (requires Docker; run once per wave, not per commit, given container startup cost)
- **Phase gate:** Full suite (both hermetic and `shibdload`-tagged) green before `/gsd-verify-work`, satisfying success criterion #1's explicit "not just a golden-file text-compare" requirement

### Wave 0 Gaps
- [ ] `internal/render/testdata/golden/` — needs the golden fixture files themselves (blocked on Open Question 1's scope decision)
- [ ] `internal/render/testdata/fixtures/` — sample `SPConfig`/`[]AppBinding` Go literals shared across test files
- [ ] `internal/render/shibdload_test.go` — the build-tag-gated container test file itself, plus the pinned image tag decision (Open Question 2/Common Pitfall 8)
- [ ] Framework install: `go get github.com/testcontainers/testcontainers-go@v0.43.0` (verify current version at execute time) + `go mod tidy`
- [ ] A standalone `xml.MarshalIndent` empirical check (Environment Availability note) — confirm the self-closing-tag finding against the real Go 1.26.0 toolchain before finalizing `collapseEmptyElements`'s exact regex

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | Out of scope — this phase has no auth logic, it's a config renderer |
| V3 Session Management | No | Out of scope |
| V4 Access Control | No | Pure library, no runtime k8s access, no RBAC surface |
| V5 Input Validation | **Yes** | `encoding/xml` struct marshaling (auto-escapes), explicit `--`-strip guard before comment marshaling (D-05), explicit hostname/value validation before `text/template.Execute` (no template auto-escaping exists for `text/template`) |
| V6 Cryptography | Partially | `crypto/sha256` is used for **change-detection only** (RENDER-09's config hash), not as an authentication/integrity boundary — no key material, no signing, no verification of untrusted input via the hash. Not a cryptographic security control in the ASVS sense; flagged here only so the planner doesn't mistake it for one and add unnecessary hardening (e.g., HMAC/keyed hashing is unnecessary — there's no adversary this hash needs to resist, it only needs to detect accidental content changes) |

### Known Threat Patterns for This Phase's Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| XML injection via unescaped CRD string fields (a hostile `AppIntegration.attributes[].name`, hostname, or entityID breaking XML structure) | Tampering | `encoding/xml` struct marshaling auto-escapes every string field passed through a normal (non-comment, non-raw) struct tag — RENDER-10 |
| `--`-in-comment causing a marshal error that aborts config generation entirely (an availability, not confidentiality, concern) | Tampering / Denial of Service | Explicit `strings.ReplaceAll(v, "--", "-")` at the input layer BEFORE marshaling (D-05) — turns a hard failure into a silent, safe transform rather than aborting the render for the whole SP over one hostile field |
| RequestMap `<Host>` fail-open on a missing/implicit scheme+port (this project's single most important carried-forward security lesson, per PITFALLS.md #1) | Elevation of Privilege (auth bypass) | RENDER-05: always emit explicit `scheme`+`port`, never rely on bare-hostname auto-expansion, even on port 443 — a negative test (unauthenticated hit on a non-standard-port protected path must not return 200) belongs to Phase 2's integration suite, but the render package's own unit test (Phase 1) must assert the attributes are always present in the rendered XML regardless of input port value |
| `text/template`'s lack of auto-escaping letting a hostile CRD string break `nginx.conf`'s directive structure | Tampering | Explicit allowlist/format validation (e.g., hostname regex) at the Go layer before the value reaches `tmpl.Execute` — there is no template-side escaping mechanism to rely on for `text/template` |

## Sources

### Primary (HIGH confidence)
- `golang/go` source, `src/encoding/xml/marshal.go` (fetched directly this session via raw.githubusercontent.com) — confirmed no self-closing-tag code path exists (`writeStart`/`writeEnd` always write paired tags), and confirmed the two distinct `--`-in-comment validation paths (`EncodeToken` rejects only `-->`; struct-tag `,comment` field rejects any `--`)
- `.planning/phases/01-shared-render-aggregation-package/01-CONTEXT.md` — locked decisions D-01 through D-11 (this repo, first-party)
- `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, `.planning/PROJECT.md`, `.planning/STATE.md` (this repo, first-party)
- `shibboleth2.xml`, `attribute-map.xml`, `nginx.conf` at repo root, branch `spike` (this repo, first-party — the actual golden fixtures)
- `gsd/operator-scaffold` branch: `api/v1alpha1/spinstance_types.go`, `api/v1alpha1/appintegration_types.go`, `go.mod`, `.github/workflows/build.yml` (this repo, first-party — confirmed Go 1.26.0 pin, existing deps, GHCR image path/tags)
- `.planning/research/{SUMMARY,STACK,ARCHITECTURE,PITFALLS,FEATURES}.md` — existing project-level research, mined for Phase-1-relevant findings without re-deriving

### Secondary (MEDIUM confidence)
- pkg.go.dev/encoding/xml — `xml.Header` constant behavior, `MarshalIndent` documented non-inclusion of the XML declaration [CITED]
- pkg.go.dev/github.com/testcontainers/testcontainers-go?tab=versions — v0.43.0, published 2026-06-19 [CITED, not independently re-verified via `go list -m`, no Go toolchain in this sandbox]
- brandur.org/fragments/cmp-or-multi-field, pkg.go.dev/slices — `cmp.Or`+`slices.SortFunc` multi-key sort idiom [CITED]
- GitHub Docs / community discussions on GHCR public-package anonymous pull behavior in GitHub Actions [CITED]
- golang/go issues #21399, #59710, #69273 (self-closing tag gap, unmerged proposals) and #9519 (xmlns namespace duplication, already cited in CONTEXT.md's own D-03) — read via WebSearch summaries, cross-checked against direct source-read above

### Tertiary (LOW confidence)
- None used as load-bearing claims — every implementation-critical claim in this document was either read directly from `golang/go` source or from a named official docs page (pkg.go.dev) this session.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — everything except `testcontainers-go`'s exact version is Go stdlib, already pinned/verified in this repo's own `go.mod`
- Architecture: HIGH — this phase's architecture is dictated almost entirely by CONTEXT.md's locked decisions (D-01 through D-11); the only new synthesis is the self-closing-tag/formatting layer, which is source-verified
- Pitfalls: HIGH for the Go-mechanics pitfalls (source-verified this session); MEDIUM for the operational pitfalls (#7 cross-product contamination, #8 GHCR visibility) since those are inferences from reading repo history, not something independently confirmable without asking Jeff

**Research date:** 2026-07-11
**Valid until:** ~90 days (Go stdlib behavior is stable and slow-moving; re-verify `testcontainers-go`'s version and the GHCR visibility/tag-pinning decisions sooner, at plan/execute time, since those are environment-state facts rather than stable API facts)
