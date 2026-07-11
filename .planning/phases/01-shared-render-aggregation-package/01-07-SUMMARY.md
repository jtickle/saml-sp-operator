---
phase: 01-shared-render-aggregation-package
plan: 07
subsystem: render
tags: [go, testcontainers-go, docker, shibboleth-sp, load-test, build-tags]

# Dependency graph
requires:
  - phase: 01-shared-render-aggregation-package (plan 03)
    provides: "render.RenderShibboleth2(cfg, winners) — the shibboleth2.xml this load test mounts"
  - phase: 01-shared-render-aggregation-package (plan 04)
    provides: "render.RenderAttributeMap(attrs) — the attribute-map.xml this load test mounts"
  - phase: 01-shared-render-aggregation-package (plan 05)
    provides: "render.RenderNginxConf(cfg) — the nginx.conf this load test mounts"
provides:
  - "internal/render/shibdload_test.go — TestShibdLoad, gated behind the shibdload build tag, mounting the render package's own rendered output into a real shib-authenticator container and asserting shibd loads with no FATAL (ROADMAP success criterion #1)"
  - "The shibdload build tag convention: any future renderer-touching plan can extend TestShibdLoad's mounted set without breaking the hermetic (Docker-free) go build/vet/test path"
affects: [phase-2-spinstance-controller, phase-5-traefik-middleware, ci-workflow-shibdload-gate]

# Tech tracking
tech-stack:
  added: ["github.com/testcontainers/testcontainers-go v0.43.0 (test-only, shibdload-tag-gated)"]
  patterns:
    - "shibd config-parse validity is proven by starting the REAL container image and waiting for shibd's own successful-startup log line ('Shibboleth initialization complete.'), not by re-implementing shibd's XML schema validation in Go — the golden byte-compare (plans 03/04/05) proves reproduction, this test proves loadability."
    - "MetadataProvider remote-fetch-failure fallback: an RFC 2606 '.invalid' TLD metadata URL guarantees the remote fetch fails fast (no live network dependency, no flakiness), and shibd's own documented backing-file fallback (XMLTooling ReloadableXMLFile) picks up the mounted static IdP metadata instead — proven empirically against the pinned image, not assumed from docs."
    - "testcontainers-go ContainerFile.Reader (not HostFilePath) injects rendered bytes directly from memory into the container at start — no host tmp files needed, keeping the test hermetic to the Go process."

key-files:
  created:
    - internal/render/shibdload_test.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "Task 1's checkpoint (GHCR image public visibility + immutable digest selection) was resolved by the orchestrator before this plan executed — the digest ghcr.io/jtickle/saml-sp-operator/shib-authenticator@sha256:0e33ee7fea4524cb3caa8744b22f05a80703d22444ef198368484dc523f41319 was confirmed to pull anonymously (docker pull with no stored GHCR credentials succeeded) and is declared as the pinnedShibAuthenticatorImage named constant in the test file, per RESEARCH.md Pitfall 8 (never the floating spike/main tag)."
  - "Used shibd's real successful-startup log line 'Shibboleth initialization complete.' (observed directly by running the pinned image interactively before writing the test) as wait.ForLog's target, rather than guessing a log string from docs — this is the same signal a config-parse FATAL would prevent the container from ever emitting."
  - "The load-test SPConfig/AppBinding/AttributeMapping fixtures are load-test-local (shibdLoadSampleSPConfig/shibdLoadSampleWinners/shibdLoadSampleAttributes in shibdload_test.go), not fixtures_test.go's shared SampleSPConfig — the load test needs a deliberately unreachable ('.invalid' TLD) IdP metadata URL and credential paths matching its own mounted files, which would be an inappropriate mutation of the golden-fixture-locked shared sample plans 03/04/05 already depend on."
  - "attribute-policy.xml, security-policy.xml, and protocols.xml are NOT mounted by this test — the pinned image's shibboleth-sp-utils Debian package already ships these at /etc/shibboleth/ (confirmed by inspecting the image's filesystem before writing the test), and shibboleth2.xml's rendered <SecurityPolicyProvider>/<ProtocolProvider>/<AttributeFilter> paths reference them by the same relative filenames the package ships. Only the render package's own three rendered artifacts, plus a throwaway sp-credentials keypair and a static IdP metadata backing file (neither of which this package renders), are mounted."
  - "sp-credentials (cert+key) are generated fresh via crypto/x509 inside TestShibdLoad on every run (generateThrowawaySelfSignedCert) — no key material is committed or reused from any prior spike/product, satisfying the public-repo hygiene constraint."

requirements-completed: [RENDER-01]

coverage:
  - id: D1
    description: "A real containerized shibd (pinned by immutable sha256 digest) mounts the render package's own rendered shibboleth2.xml + attribute-map.xml + nginx.conf and reaches its successful-startup log line with no FATAL anywhere in the container's combined logs — ROADMAP success criterion #1, not just a golden-file text-compare"
    requirement: "RENDER-01"
    verification:
      - kind: integration
        ref: "internal/render/shibdload_test.go#TestShibdLoad (go test -tags shibdload ./internal/render/... -run TestShibdLoad -v)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The shibdload build tag keeps plain go build/vet/test ./internal/render/... hermetic and Docker-free, and go list -deps ./internal/render/ (non-test) carries no testcontainers or k8s.io dependency"
    verification:
      - kind: unit
        ref: "go build ./internal/render/ && go vet ./internal/render/ && go test ./internal/render/... (no -tags) && test -z \"$(go list -deps ./internal/render/ | grep -E 'testcontainers|k8s.io')\""
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-07-11
status: complete
---

# Phase 1 Plan 7: Real-shibd Load Test (shibdload_test.go) Summary

**`TestShibdLoad` (testcontainers-go, `//go:build shibdload`) starts the pinned, sha256-digest-locked `shib-authenticator` GHCR image and mounts the render package's own rendered `shibboleth2.xml`/`attribute-map.xml`/`nginx.conf` into it, proving a real `shibd` loads them with no FATAL — closing the golden-byte-compare-vs-actually-loadable gap that is ROADMAP success criterion #1.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-11
- **Tasks:** 2 (Task 1 checkpoint pre-resolved by orchestrator; Task 2 executed)
- **Files modified:** 3 (`internal/render/shibdload_test.go` created, `go.mod`/`go.sum` updated)

## Accomplishments
- Confirmed the pinned digest (`ghcr.io/jtickle/saml-sp-operator/shib-authenticator@sha256:0e33ee7fea4524cb3caa8744b22f05a80703d22444ef198368484dc523f41319`, handed down pre-resolved from the orchestrator's checkpoint) pulls anonymously with `docker logout ghcr.io` first — the GHCR package is confirmed Public.
- Inspected the pinned image's filesystem directly (`docker run --entrypoint sh ... ls /etc/shibboleth/`) to establish ground truth on what the Debian `shibboleth-sp-utils` package already ships (`attribute-policy.xml`, `security-policy.xml`, `protocols.xml`, `attribute-map.xml`) versus what must be mounted at runtime (`shibboleth2.xml`, `attribute-map.xml`, `nginx.conf` per the Dockerfile's own comment) — this determined the minimal correct mount set.
- Interactively validated the exact fail-safe mechanism this test relies on by running `shibd -t` and the full container against a hand-rendered sample: a `.invalid`-TLD `MetadataProvider` URL fails DNS resolution fast, and shibd's `ReloadableXMLFile` backing-file fallback (`OpenSAML.MetadataProvider.XML : trying backup file...`) picks up the mounted static IdP metadata — confirmed via real logs, not assumed from documentation.
- Implemented `TestShibdLoad`: renders all three artifacts via the package's own `RenderShibboleth2`/`RenderAttributeMap`/`RenderNginxConf`, generates a fresh throwaway self-signed SP keypair via `crypto/x509` (never reused/committed), mounts everything via `testcontainers.ContainerFile.Reader` (in-memory, no host tmp files), waits on shibd's real `"Shibboleth initialization complete."` log line, and asserts no `"FATAL"` substring appears anywhere in the container's combined logs.
- Ran the gated test twice for stability (`go test -tags shibdload ./internal/render/... -run TestShibdLoad -v`) — both runs passed in ~4.5s with a clean container lifecycle (created, ready, stopped, terminated).
- Verified the hermetic boundary holds: `go build`/`go vet`/`go test ./internal/render/...` (no `-tags`) never touch Docker, and `go list -deps ./internal/render/` (non-test package) contains zero `testcontainers`/`k8s.io` entries.

## Task Commits

1. **Task 1: Confirm GHCR image is public and pin an immutable sha tag** — pre-resolved by the orchestrator (see `<checkpoint_resolved>` in the execution prompt); no commit, digest handed directly to Task 2.
2. **Task 2: Build-tag-gated real-shibd load test (shibdload_test.go) — D-09/RENDER-01**
   - `83290a9` test(01-07): add build-tag-gated real-shibd load test (shibdload_test.go)

_This plan is `type="execute"` (D-09/D-10 checkpoint + implementation), not a TDD-gated plan — Task 2 is a single `auto` task with an automated `<verify>` gate, not a RED/GREEN cycle._

## Files Created/Modified
- `internal/render/shibdload_test.go` - `TestShibdLoad` + `generateThrowawaySelfSignedCert` + `shibdLoadSampleSPConfig`/`shibdLoadSampleWinners`/`shibdLoadSampleAttributes`/`shibdLoadIdPMetadata` fixtures + the `pinnedShibAuthenticatorImage`/`shibdLoadSuccessLogLine` constants, all behind `//go:build shibdload`
- `go.mod` / `go.sum` - `github.com/testcontainers/testcontainers-go v0.43.0` added as a direct (test-only) dependency via `go get` + `go mod tidy`

## Decisions Made
See `key-decisions` in frontmatter: Task 1's checkpoint resolution and digest source; the real observed shibd startup log line as the wait target; load-test-local (not shared) SPConfig/AppBinding fixtures; relying on the image's own shipped `attribute-policy.xml`/`security-policy.xml`/`protocols.xml` rather than re-mounting them; fresh throwaway sp-credentials generated per test run.

## Deviations from Plan

None — plan executed exactly as written. Task 1's checkpoint was already resolved by the orchestrator (per the execution prompt's `<checkpoint_resolved>` block) before this agent started, so no human-verify pause was needed; Task 2's `<action>`/`<acceptance_criteria>` were implemented and verified as specified, including the `go list -deps` dependency-boundary check and the actual `go test -tags shibdload ... -run TestShibdLoad -v` run (twice, for stability confidence) with a real Docker daemon.

## Issues Encountered
None. The one open question going in — whether shibd's `MetadataProvider` remote-fetch failure would be FATAL or gracefully fall back to the backing file — was resolved empirically by running the pinned image interactively (`shibd -t`, then a full `supervisord` startup) before writing the Go test, rather than guessing.

## User Setup Required
None for this plan's execution — the GHCR public-visibility checkpoint (Task 1) was a one-time, already-completed setup action, not a per-run requirement. No further external service configuration is needed to run `TestShibdLoad`; it only requires a local Docker daemon.

## Next Phase Readiness
- ROADMAP success criterion #1 ("a real containerized shibd parses and loads successfully, not just a golden-file text-compare") is satisfied — `TestShibdLoad` is the durable proof, re-runnable via `go test -tags shibdload ./internal/render/... -run TestShibdLoad -v` any time the render package's output shape changes.
- D-11's build-time fail-safe net (1 of 3) is in place. The `shibdload` build tag is now an established convention any future `internal/render` plan can extend (e.g., mounting additional rendered artifacts) without touching the hermetic `go build`/`go vet`/`go test` path.
- A CI workflow step running `go test -tags shibdload ./internal/render/... -run TestShibdLoad -v` (Docker available on the runner) is the natural next integration point — not created by this plan, since this plan's `files_modified` scope was limited to `shibdload_test.go`/`go.mod`/`go.sum`.
- `.planning/STATE.md` and `.planning/ROADMAP.md` are intentionally left untouched by this agent (planning/code branch split — the phase orchestrator owns those on `main`). Requirement `RENDER-01` and ROADMAP crit 1 should be marked satisfied by this plan's work when the orchestrator next updates those files on `main`.
- No blockers.

---
*Phase: 01-shared-render-aggregation-package*
*Completed: 2026-07-11*

## Self-Check: PASSED

Both created/modified files confirmed present on disk (`internal/render/shibdload_test.go`, `.planning/phases/01-shared-render-aggregation-package/01-07-SUMMARY.md`); both commits (`83290a9`, `f9aafc4`) confirmed present in `git log --oneline --all`. `go build ./...`, `go vet ./...`, `go test ./...` (hermetic, no Docker) all exit clean; `go test -tags shibdload ./internal/render/... -run TestShibdLoad -v` (Docker available) passed twice in a row (~4.5s each), with shibd reaching `"Shibboleth initialization complete."` and no `FATAL` in the container logs.
