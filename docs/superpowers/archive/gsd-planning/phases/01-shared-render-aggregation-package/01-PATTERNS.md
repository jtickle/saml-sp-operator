# Phase 1: Shared Render & Aggregation Package - Pattern Map

**Mapped:** 2026-07-11
**Files analyzed:** 10 (new; no modifications — `internal/render` does not exist yet)
**Analogs found:** 0 exact in-repo code analogs / 10 — this is a **greenfield pure-Go package**;
no prior Go rendering/marshaling code exists anywhere in this repo on any branch. The closest
"analogs" are (a) the golden config fixtures that define the exact structural output target,
(b) the CRD types that define the semantic vocabulary (read-only, never imported), and
(c) Go-stdlib-verified code patterns from RESEARCH.md (sourced from `golang.org/x/...` stdlib
source and pkg.go.dev during this session, not copied from any file in this repo).

## Why there are no code analogs

`git ls-tree -r spike` and `git ls-tree -r gsd/operator-scaffold` were both searched for any
existing `.go` file under `internal/render`, any `encoding/xml` usage, any `text/template`
usage, or any collision/sort helper — none exist. The `spike` branch's Go surface is limited to
`.github/workflows/build.yml` (CI, not Go source) and the `spike/` binary artifacts (Dockerfile,
supervisord, PHP IdP test app — not Go). The `gsd/operator-scaffold` branch's Go surface is
`api/v1alpha1/*.go` (CRD types, k8s-coupled, D-01 forbids importing them) and
`internal/controller/*.go` (kubebuilder-scaffolded stubs, no reconcile logic yet — nothing to
pattern-match against for render/marshal/sort/hash concerns). Do not treat this as a gap in the
search; it is the actual state of the codebase pre-Phase-1.

Because there is no in-repo Go code to copy from, this PATTERNS.md instead pins:
1. The **exact byte-target** (golden fixtures) each renderer must reproduce, with concrete line
   citations for every element/attribute shape.
2. The **semantic vocabulary source** (CRD types) each plain-Go input struct mirrors — read-only
   reference, never an import.
3. The **stdlib code patterns** RESEARCH.md already verified against Go 1.26 source/pkg.go.dev
   for the two genuinely new mechanisms this phase needs (self-closing-tag collapse,
   multi-key deterministic sort, length-prefixed hash) — these are the actual "patterns to
   copy from" in the absence of in-repo precedent.

## File Classification

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|-----------------|----------------|
| `internal/render/types.go` | model | transform (plain-Go input/output shapes) | `api/v1alpha1/appintegration_types.go` + `api/v1alpha1/spinstance_types.go` (`gsd/operator-scaffold`) | semantic-only — mirror field meaning, **never import** |
| `internal/render/resolve.go` | service | transform (pure sort/partition, no I/O) | none in-repo; RESEARCH.md Pattern 2 (`cmp.Or`+`slices.SortFunc`, stdlib-verified) | no-analog — stdlib pattern |
| `internal/render/resolve_test.go` | test | transform | none in-repo | no-analog |
| `internal/render/shibboleth2.go` | transform/utility | file-I/O (bytes out, no disk I/O itself) | `shibboleth2.xml` (repo root, golden target) | structural analog only (target, not source code) |
| `internal/render/attributemap.go` | transform/utility | file-I/O | `attribute-map.xml` (repo root, golden target) | structural analog only |
| `internal/render/nginxconf.go` | transform/utility | file-I/O | `nginx.conf` (repo root, golden target) | structural analog only |
| `internal/render/clearlist.go` | utility | transform (pure value computation) | none — RENDER-08 scope note (Pitfall 6) | no-analog, scope-limited |
| `internal/render/xmlformat.go` | utility | transform | none in-repo; RESEARCH.md Pattern 1 (`collapseEmptyElements`, stdlib-verified against Go 1.26 source) | no-analog — stdlib-gap workaround |
| `internal/render/confighash.go` | utility | transform | none in-repo; RESEARCH.md Pattern 3 (length-prefixed `crypto/sha256` concatenation) | no-analog — stdlib pattern |
| `internal/render/shibdload_test.go` | test | event-driven (container lifecycle, build-tag gated) | `edge/testenv/docker-compose.yml` (`spike` branch, repo root) — **shape reference only, do not reuse verbatim (cross-contaminated with an unrelated Tickle Technologies product, see RESEARCH.md Pitfall 7)** | shape-analog, contaminated — use structure not content |

## Pattern Assignments

### `internal/render/types.go` (model, transform)

**Analog:** `api/v1alpha1/appintegration_types.go` + `api/v1alpha1/spinstance_types.go` on branch
`gsd/operator-scaffold` (read via `git show gsd/operator-scaffold:api/v1alpha1/...` — not in the
`spike` working tree).

**Do not import these files or `k8s.io/apimachinery`/`metav1`.** Mirror only the *field
semantics* into plain-Go equivalents:

| CRD field (source) | Plain-Go render equivalent | Why plain-Go, not CRD type |
|---|---|---|
| `AppIntegrationSpec.Attributes []AttributeMapping{Name, Header}` | `render.AttributeMapping{Name, Header string}` | Same shape, zero k8s import needed |
| `AppIntegrationSpec.RequireSession *bool` | `render.AppBinding.RequireSession bool` (caller resolves the default-true-when-nil) | Render package shouldn't encode the CRD's `*bool`-omit-means-default convention itself |
| `metav1.ObjectMeta.CreationTimestamp` (second-granular RFC3339) | `render.AppBinding.CreatedAtUnix int64` (pre-truncated by caller) | D-01: `createdAt`/UID live on ObjectMeta, not spec — package needs a synthesized input regardless; RESEARCH.md Pitfall 3 explicitly requires the caller to truncate to whole seconds before this boundary |
| `metav1.ObjectMeta.UID` (`types.UID`, a string) | `render.AppBinding.UID string` | Avoid the `types.UID` type import; a bare string carries the same tiebreak value (RESEARCH.md Pattern 2 code example already types it this way) |
| New `AppIntegration.spec.priority int32` (not yet in the scaffold CRD — D-07 says planner's call on when it lands) | `render.AppBinding.Priority int32` | Sort key input regardless of when the CRD field lands |
| `(hostname, path)` — **does not exist on the CRD spec at all** (resolved from the HTTPRoute by the controller) | `render.AppBinding.Hostname, Path string` | D-01 explicitly calls this out: the package "physically needs a synthesized input regardless of the dependency question" |
| `SPInstanceSpec.EntityID string` | `render.SPConfig.EntityID string` | Direct 1:1 field mirror |
| `SPInstanceSpec.IdP IdPConfig{MetadataURL, SigningCert, EntityID}` | `render.IdPConfig{MetadataURL, EntityID string}` (signing-cert file path resolved by caller, not fetched by this package) | Package renders paths/values, never does k8s Secret I/O |

### `internal/render/resolve.go` (service, transform)

**No in-repo analog.** Use RESEARCH.md's stdlib-verified Pattern 2 directly (already reflects
D-06/D-07 exactly — `Resolve(bindings []AppBinding) (Resolution, error)`, sort key
`(priority desc, createdAt asc, UID asc)`):

```go
import ("cmp"; "slices")

func rankOrder(bindings []AppBinding) []AppBinding {
	out := slices.Clone(bindings)
	slices.SortFunc(out, func(a, b AppBinding) int {
		return cmp.Or(
			cmp.Compare(b.Priority, a.Priority),           // desc
			cmp.Compare(a.CreatedAtUnix, b.CreatedAtUnix),  // asc
			cmp.Compare(a.UID, b.UID),                      // asc, final tiebreak
		)
	})
	return out
}
```

Note (RESEARCH.md Pattern 2 trade-off, keep as a code comment in the real file so a future
reader doesn't "fix" it): `slices.SortFunc` is not stability-guaranteed, but the UID tiebreak
makes the comparator a strict total order, so stability doesn't matter here — do not switch to
`SortStableFunc` under a mistaken belief it's needed.

**resolve_test.go must include** (RESEARCH.md Pitfall 3, D-07 explicit requirement): a
same-second `CreatedAtUnix` tiebreak case — two bindings with identical truncated timestamp,
differing UID, asserting the lower-UID one wins.

### `internal/render/shibboleth2.go` (transform/utility, file-I/O)

**Analog (structural target, not source code):** `shibboleth2.xml` (repo root, `spike` branch,
also present in the current `spike` working tree — 111 lines).

Concrete shapes the marshaler must reproduce (line-cited from the fixture read this session):

- **Root element** (lines 16-18): `<SPConfig xmlns="urn:mace:shibboleth:3.0:native:sp:config" xmlns:conf="urn:mace:shibboleth:3.0:native:sp:config" clockSkew="180">` — this is D-03's literal-`xmlns`-attribute case: declare `xmlns`/`xmlns:conf` as plain string attrs (`xml:"xmlns,attr"`, `xml:"xmlns:conf,attr"`) on the root struct ONLY.
- **RequestMapper/RequestMap/Host** (lines 36-41): `<Host name="..." scheme="https" port="30443" authType="shibboleth" requireSession="true"/>` — self-closing, mandatory `scheme`+`port` even on default ports (RENDER-05, RESEARCH.md Code Examples `hostElement` struct is the exact target shape).
- **ApplicationDefaults** (lines 48-49): `entityID` + `REMOTE_USER` attrs — REMOTE_USER is a space-joined attribute-id list (`"email uid"`), the IdP's principal attribute list.
- **Sessions** (lines 67-69): `lifetime`, `timeout`, `relayState`, `checkAddress`, `handlerSSL`, `cookieProps`, and **fully-qualified `handlerURL`** including non-standard port — spike fix M, D-04's `nginx.conf` self-URL derivation must agree with this value.
- **SSO/Logout/Handler children** (lines 74-80): `<SSO entityID="...">SAML2</SSO>` has chardata (must NOT self-close); `<Handler type="..." Location="..." .../>` self-closes (no chardata) — this is the exact positive/negative pair RESEARCH.md Pitfall 1 says the `collapseEmptyElements` unit test must cover.
- **MetadataProvider / AttributeExtractor / AttributeResolver / AttributeFilter / CredentialResolver** (lines 93-104): all self-closing, attribute-only elements — same collapse requirement.
- **SecurityPolicyProvider / ProtocolProvider** (lines 107-108): self-closing, siblings of `ApplicationDefaults` at the `SPConfig` root level, not children of it.
- **Prolog** (line 1): `<?xml version="1.0" encoding="UTF-8"?>` — not emitted by `xml.MarshalIndent`; prepend `xml.Header` (RESEARCH.md Pitfall 2).
- **`SP_HOST_PLACEHOLDER`** appears 3x (lines 38, 48, 69) — every occurrence must be substituted from the same `render.SPConfig`/`AppBinding` hostname value; a mismatch between any two would itself be the fail-safe-rollout bug D-11 exists to prevent.

Per RESEARCH.md Pitfall 5: do not attempt to reproduce the fixture's hand-authored prose
comments (lines 2-15, 20-35, 43-47, 51-66, 71-73, 87-92) byte-for-byte — those are spike
narration, not a generation target. The byte-for-byte target is the semantic XML tree; build the
package's own `testdata/golden/` fixtures (comment-minimal, machine-formatted) as the actual
compare target, using this root fixture only as the structural reference. **This is an open
question the planner should confirm with Jeff before locking `testdata/` layout, not a silently
resolved one.**

### `internal/render/attributemap.go` (transform/utility, file-I/O)

**Analog (structural target):** `attribute-map.xml` (repo root — 38 lines).

- **Root element** (lines 31-32): `<Attributes xmlns="urn:mace:shibboleth:2.0:attribute-map" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` — second literal-`xmlns`(+`xmlns:xsi`) root case D-03 calls out; same root-struct-only attribute pattern as `shibboleth2.go`.
- **Attribute children** (lines 33-36): `<Attribute name="email" id="email"/>` — self-closing, two plain string attrs, one struct type reused N times via a slice field (`Attributes []Attribute \`xml:"Attribute"\``).
- Fixture's hand-aligned columns (`name="email"     id="email"`) are cosmetic spike formatting — `encoding/xml` cannot and should not reproduce column alignment (RESEARCH.md Pitfall 5); target the semantic attribute set/order only.
- This is the file D-08 flags as `reloadChanges="false"` (shibboleth2.xml:98 references it) — an attribute-only change must still force the confighash to change and thus a pod roll; do not exclude this file's bytes from `confighash.go`'s input set.

### `internal/render/nginxconf.go` (transform/utility, file-I/O)

**Analog (structural target):** `nginx.conf` (repo root — 111 lines).

- Rendered via `text/template`, not `encoding/xml` (D-04) — this file has almost no CRD-derived
  free-text fields (mostly static FastCGI param blocks), so the auto-escaping gap (RESEARCH.md
  Code Examples, `text/template` guard section) mainly matters for any hostname interpolated into
  a `server_name`/comment context — apply `validateHostname` (regex allowlist) before the value
  reaches `tmpl.Execute`, per RESEARCH.md's explicit guard pattern.
- **`/Shibboleth.sso` block** (lines 43-72): the `fastcgi_param HTTPS on`, `SERVER_PORT`,
  `SERVER_NAME`, `HTTP_HOST $host:<port>` overrides that MUST precede `include fastcgi_params` —
  spike fix M/N territory; the template's per-app port value must match the same external port
  used to build `handlerURL` in `shibboleth2.go` (RENDER-02's self-URL-value derivation is shared
  input feeding both files — keep as one computed value, not two independently-typed literals).
- **`/authcheck` block** (lines 88-108): rewrites `X-Forwarded-*` into FastCGI params for the
  ForwardAuth ForwardAuth subrequest — static structure, no per-app templating beyond the same
  port/host values already covered above.
- `nginx.conf` fixture has **no** `more_clear_input_headers`/clear-list directive at all — per
  RESEARCH.md Pitfall 6, RENDER-08's nginx-glob branch has no consumer in this file; do not add
  one here. `clearlist.go` computes the value only, this template doesn't render it.

### `internal/render/xmlformat.go` (utility, transform)

**No in-repo analog — the load-bearing new finding this phase must implement.** Use
RESEARCH.md Pattern 1 verbatim as the starting point (already verified against Go 1.26
`src/encoding/xml/marshal.go` source this session — `encoding/xml` never emits `<Foo/>`, only
`<Foo></Foo>`, confirmed no code path exists for the short form):

```go
var emptyElementRE = regexp.MustCompile(`(?s)<([A-Za-z][\w:.-]*)((?:\s+[\w:.-]+="[^"]*")*)\s*></([A-Za-z][\w:.-]*)>`)

func collapseEmptyElements(b []byte) []byte {
	return emptyElementRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := emptyElementRE.FindSubmatch(m)
		name, attrs, closeName := sub[1], sub[2], sub[3]
		if string(name) != string(closeName) {
			return m
		}
		return []byte("<" + string(name) + string(attrs) + "/>")
	})
}
```

Unit-test both directions explicitly: `<Foo x="1"></Foo>` → `<Foo x="1"/>` (collapse), and
`<SSO ...>SAML2</SSO>` (chardata present) → unchanged (must NOT collapse) — RESEARCH.md Pitfall
1's exact required test pair, directly visible in the `shibboleth2.xml` fixture (`<Handler .../>`
vs `<SSO ...>SAML2</SSO>` are adjacent lines 74-80, a real positive/negative pair already in the
target fixture).

### `internal/render/confighash.go` (utility, transform)

**No in-repo analog.** Use RESEARCH.md Pattern 3 (length-prefixed `crypto/sha256`
concatenation) verbatim — covers D-08's requirement that `attribute-map.xml` bytes are included
(not just `shibboleth2.xml`+`nginx.conf`, which the scaffold's existing doc-comment on
`SPInstanceStatus.ConfigHash` — `gsd/operator-scaffold`, `api/v1alpha1/spinstance_types.go` line
~137, "hash of the rendered shibboleth2.xml + nginx.conf" — incorrectly omits; D-08 flags this as
a doc bug to fix when Phase 2 touches that file, not something Phase 1 edits):

```go
type ConfigFile struct {
	Name  string
	Bytes []byte
}

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

Caller passes files in a fixed explicit order (`shibboleth2.xml, nginx.conf, attribute-map.xml`)
rather than this function sorting internally — keeps the hash input order self-documenting at
the call site (RESEARCH.md Pattern 3 trade-off note).

### `internal/render/shibdload_test.go` (test, event-driven, build-tag gated)

**Analog:** `edge/testenv/docker-compose.yml` on branch `spike` (repo root, also present in the
current `spike` checkout) — **shape reference only.**

**RESEARCH.md Pitfall 7, confirmed by direct read this session:** this file (and its siblings
`edge/Dockerfile`, `edge/nginx.validate.conf`) is cross-contaminated with an unrelated Tickle
Technologies product (an unrelated product's delegated-auth testbed — its backend server, `simplesamlphp`, an internal thread), not this project's fixtures. **Do not reuse it verbatim.** Reuse only its
bind-mount *shape*: `shibboleth2.xml` / `attribute-map.xml` / `attribute-policy.xml` /
`nginx.conf` / `sp-credentials/` mounted read-only into the SP container — construct the
equivalent mounts via `testcontainers-go`'s `ContainerRequest.Files`, using this repo's own
root-level fixtures (`shibboleth2.xml`, `attribute-map.xml`, `nginx.conf`, already read above)
as the reference shape, and this package's own rendered output as the actual mounted content.

Since this is a **public** repo, do not carry over any product-specific naming/thread-id/product
references into the new test file — even though RESEARCH.md assesses `edge/` as not
credential-bearing, treat the whole file as non-authoritative content, not just non-authoritative
structure.

Pin an explicit immutable short-sha GHCR tag (`ghcr.io/jtickle/saml-sp-operator/shib-authenticator:<sha>`),
not the floating `spike` tag (RESEARCH.md Pitfall 8) — confirm the package's GHCR visibility is
Public before relying on anonymous CI pulls (one-time manual gate, outside this phase's code,
flag as `checkpoint:human-verify` in the plan).

---

## Shared Patterns

### Zero k8s dependency (D-01/D-02)
**Source:** absence pattern — verified no `k8s.io/*` or `api/v1alpha1` import exists or should
exist anywhere under `internal/render/`.
**Apply to:** every file in this phase, without exception, including the build-tag-gated test
file (`testcontainers-go` is fine — it's a Docker-only lib, not a Kubernetes client, confirmed in
RESEARCH.md's Package Legitimacy Audit).
**Enforcement suggestion for the plan:** a `go vet`/import-lint check or a simple `grep -R
'k8s.io\|api/v1alpha1' internal/render` CI guard, since this is a construction-over-convention
concern per the project's cross-cutting-concerns default (nothing currently fails loud if someone
adds a k8s import six months from now).

### No map ranging in the render path
**Source:** RESEARCH.md Anti-Patterns section (restates project `PITFALLS.md` #10).
**Apply to:** `resolve.go`, `shibboleth2.go`'s RequestMap aggregation (RENDER-04), and any future
ordering logic — any `for k, v := range someMap` feeding rendered output breaks RENDER-09's hash
stability (ROADMAP success criterion #4).

### Injection safety: escape-by-construction + explicit `--` guard
**Source:** D-05 + RESEARCH.md Pitfall 4 (two distinct comment-validation code paths in
`encoding/xml` — struct-tag `,comment` fields reject any `--`; low-level `EncodeToken` only
rejects `-->`).
**Apply to:** `shibboleth2.go`, `attributemap.go` (auto-escaping is free via normal struct-tag
fields) and any place a CRD-derived string is ever routed into an XML comment — use the
struct-tag `,comment` path only, plus keep the `strings.ReplaceAll(v, "--", "-")` input-layer
strip as defense-in-depth so one hostile field doesn't abort the entire render.

### Self-closing tag collapse
**Source:** `xmlformat.go` (this phase, first-of-its-kind in this repo).
**Apply to:** both `shibboleth2.go` and `attributemap.go` — the one non-obvious plumbing piece
shared by every XML-producing file in this package.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/render/resolve.go` | service | transform | No sort/collision logic exists anywhere in this repo; RESEARCH.md Pattern 2 (stdlib `cmp`/`slices`) is the only precedent available |
| `internal/render/xmlformat.go` | utility | transform | Genuinely new problem (stdlib gap); no prior art in-repo or in stdlib |
| `internal/render/confighash.go` | utility | transform | No hashing code exists in this repo yet |
| `internal/render/clearlist.go` | utility | transform | RENDER-08's nginx-glob branch has zero current-repo consumer (Pitfall 6); Traefik-enumerate branch's consumer (Middleware CRD) doesn't land until Phase 5 |

## Metadata

**Analog search scope:** `git ls-tree -r spike`, `git ls-tree -r gsd/operator-scaffold` (full
repo tree on both relevant branches); root-level golden fixtures read directly
(`shibboleth2.xml`, `attribute-map.xml`, `nginx.conf`); CRD types read via
`git show gsd/operator-scaffold:api/v1alpha1/{appintegration,spinstance}_types.go`; controller
stubs (`internal/controller/spinstance_controller.go`) checked and confirmed to be
kubebuilder-scaffold boilerplate with no reconcile logic to pattern-match.
**Files scanned:** ~120 tracked files across two branches (tree listings), 4 read in full
(3 golden fixtures + 2 CRD type files combined read).
**Pattern extraction date:** 2026-07-11

READ_ROOT: /home/claude/saml-sp-operator
