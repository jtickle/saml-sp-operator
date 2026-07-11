---
phase: 1
slug: shared-render-aggregation-package
status: approved
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-11
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (`go test`) — pure-Go package, no third-party test framework needed. The scaffold's ginkgo/gomega are for later controller/envtest suites, not this phase. |
| **Config file** | none — `go test ./internal/render/...` needs no config; the gated load test uses only the `-tags shibdload` flag |
| **Quick run command** | `go test ./internal/render/...` |
| **Full suite command** | `go test -tags shibdload ./internal/render/... -v` (requires Docker + network to pull the pinned GHCR shib-authenticator image) |
| **Estimated runtime** | ~5s hermetic; ~30–60s with the container load layer |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/render/...` (hermetic, no Docker — fast)
- **After every plan wave:** Run `go test -tags shibdload ./internal/render/... -v` (Docker; once per wave, not per commit, given container startup cost)
- **Before `/gsd-verify-work`:** Both hermetic and `shibdload`-tagged suites must be green — satisfies success criterion #1 ("not just a golden-file text-compare")
- **Max feedback latency:** ~5 seconds (hermetic layer)

---

## Per-Task Verification Map

| Requirement | Behavior | Test Type | Automated Command | File Exists |
|-------------|----------|-----------|-------------------|-------------|
| RENDER-01 | shibboleth2.xml renders correct structure | golden-file (unit) | `go test ./internal/render/... -run TestRenderShibboleth2` | ❌ W0 |
| RENDER-01 | rendered shibboleth2.xml is loadable by real shibd | gated integration | `go test -tags shibdload ./internal/render/... -run TestShibdLoad` | ❌ W0 |
| RENDER-02 | self-URL values (scheme/name/port/handlerURL) consistent | unit | `go test ./internal/render/... -run TestSelfURLConsistency` | ❌ W0 |
| RENDER-03 | attribute-map.xml renders correct structure | golden-file (unit) | `go test ./internal/render/... -run TestRenderAttributeMap` | ❌ W0 |
| RENDER-04 | RequestMap ordering: exact-Host before HostRegex, most-specific-path-first | unit | `go test ./internal/render/... -run TestRequestMapOrdering` | ❌ W0 |
| RENDER-05 | every Host carries explicit scheme+port, incl. default ports | unit (negative case: default-port 443 still shows explicit attrs) | `go test ./internal/render/... -run TestHostSchemePort` | ❌ W0 |
| RENDER-06 | deterministic winner (priority desc, createdAt asc, UID asc); same-second tiebreak | property/determinism | `go test ./internal/render/... -run TestResolveDeterminism` | ❌ W0 |
| RENDER-07 | nginx.conf renders correctly | golden-file (unit) | `go test ./internal/render/... -run TestRenderNginxConf` | ❌ W0 |
| RENDER-08 | clear-list value correctness per attachment model (Traefik enumerate / nginx glob) | unit | `go test ./internal/render/... -run TestClearList` | ❌ W0 |
| RENDER-09 | sha256 hash stable across map-order reshuffles, changes iff content changes, includes attribute-map.xml | property (determinism + change-sensitivity) | `go test ./internal/render/... -run TestConfigHash` | ❌ W0 |
| RENDER-10 | hostile strings (`--`, `<`, `&`, `]]>`) never produce invalid/FATAL-ing XML | adversarial fuzz/property | `go test ./internal/render/... -run TestInjectionSafety` | ❌ W0 |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/render/testdata/golden/` — the golden fixture files themselves (scope: project-native semantic-tree fixtures, per RESEARCH Open Question 1 recommendation — confirm before writing)
- [ ] Shared sample `SPConfig` / `[]AppBinding` fixtures — as exported Go builder functions in `internal/render/fixtures_test.go` (plan 01-01) or under `internal/render/testdata/fixtures/` (layout is Claude's Discretion per CONTEXT.md)
- [ ] `internal/render/shibdload_test.go` — the `//go:build shibdload` gated container test file + pinned immutable image tag (not the floating `spike` tag)
- [ ] `go get github.com/testcontainers/testcontainers-go@<current>` + `go mod tidy` (verify version at execute time) — or a hand-rolled `os/exec` docker wrapper (discretion point, RESEARCH Open Question 3)
- [ ] Empirical `xml.MarshalIndent` self-closing-tag check against the real Go 1.26.0 toolchain before finalizing `collapseEmptyElements`

*Golden-file, property/determinism, and injection tests are all automatable — no manual-only verification in this phase.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| — | — | — | All phase behaviors have automated verification. |

*The shibd-load layer is automated via the `shibdload` build tag; it is CI-gated, not manual.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 5s (hermetic layer)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-11 (plan-checker VERIFICATION PASSED; every RENDER-01…RENDER-10 has an automated verify command)
