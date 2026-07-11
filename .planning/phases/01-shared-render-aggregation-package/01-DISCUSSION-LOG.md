# Phase 1: Shared Render & Aggregation Package - Discussion Log (Assumptions Mode)

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions captured in CONTEXT.md — this log preserves the analysis.

**Date:** 2026-07-10
**Phase:** 01-shared-render-aggregation-package
**Mode:** assumptions
**Areas analyzed:** Package API shape, XML/config generation, Collision + config-hash, Testing strategy

## Assumptions Presented

### Package API shape
| Assumption | Confidence | Evidence |
|------------|-----------|----------|
| Plain-Go input struct, no `api/v1alpha1` import | Likely → Confident | `appintegration_types.go` (TargetRef names route, no host/path); REQUIREMENTS.md "no k8s deps"; RequestMap needs resolved host/path + ObjectMeta |

### XML/config generation
| Assumption | Confidence | Evidence |
|------------|-----------|----------|
| `encoding/xml` + literal-`xmlns`-attribute (root only) | Unclear → Confident | Live Go 1.26 experiment (byte-for-byte match); Go issue #9519; spike `shibboleth2.xml:16-18` |
| `text/template` for nginx.conf | Confident | RENDER-07; spike `nginx.conf` |
| `--`-in-comments guard required at input layer | Confident | Research: Go marshaler returns error + emits nothing on `--` in comment; spike fix K |

### Collision + config-hash
| Assumption | Confidence | Evidence |
|------------|-----------|----------|
| Split `Resolve`/`Render`; structured `Conflicts[]`; independently callable | Likely → Confident | DESIGN §7; ROADMAP Phase 3 crit #2 ("neither controller writing into the other's status"); APP-04 |
| Sort key `(priority desc, createdAt asc, UID asc)` | Confident | RENDER-06; `metav1.Time` second-granularity → UID tiebreak load-bearing |
| Hash over shibboleth2.xml + nginx.conf + **attribute-map.xml** | Confident (corrected) | spike `shibboleth2.xml:98` `reloadChanges="false"`; scaffold `configHash` comment incomplete |

### Testing strategy
| Assumption | Confidence | Evidence |
|------------|-----------|----------|
| Golden byte-compare + gated real-shibd container load test | Likely → Confident | ROADMAP Phase 1 crit #1; `edge/testenv/docker-compose.yml`; public GHCR image + ubuntu-latest Docker (`.github/workflows/build.yml`) |

## Corrections Made

### Package API shape (rationale corrected, decision unchanged)
- **Original assumption:** plain-Go struct because "no k8s dep" requirement + data availability.
- **User steer:** questioned why "no k8s dep" was a requirement at all ("this is a kubernetes solution"). Surfaced the real reason — the render core is the shared seam between the k8s operator and a planned standalone single-container tool (per a separate product's demo container that already vendors this repo's SP image). Decision unchanged; rationale upgraded and recorded in PROJECT.md.

### Collision winner policy — priority field added
- **Original assumption:** implicit oldest-wins (`createdAt`, UID tiebreak), no priority field for v1.
- **User correction:** add an explicit `priority` field now. Designed as `AppIntegration.spec.priority` int32, higher-wins, default 0 (k8s `PriorityClass` idiom, backward-compatible). Sort key extended to `(priority desc, createdAt asc, UID asc)`. RENDER-06 updated.

### Fail-safe rollout elevated to a named requirement
- **User steer:** wants the ingress-nginx property — invalid config can never replace a healthy pod. Mapped to a three-net guarantee (build-time load test / admission CEL / runtime readiness + `maxUnavailable: 0`). To guarantee the runtime piece is not forgotten, added **SPI-07** to REQUIREMENTS.md mapped to Phase 2 (a traced requirement, not a decision-log note) + ROADMAP Phase 2 requirements/success-criterion updated + PROJECT.md decision logged. Count 31 → 32.

## Auto-Resolved
Not applicable — interactive session.

## External Research
- **Go `encoding/xml` namespace emission** — the literal-`xmlns`-attribute approach (approach b) produces the Shibboleth default-namespace document byte-for-byte in Go 1.26; the `xml.Name{Space}` approach re-declares `xmlns` on every child (Go issue #9519, unresolved a decade). Verified by a live Go experiment. (Source: golang/go#9519, pkg.go.dev/encoding/xml, live test.)
- **`--` in XML comments** — Go's marshaler does not escape `--`; it returns `xml: comments must not contain "--"` and emits nothing. Requires an explicit input-layer strip on CRD-derived comment values. (Source: pkg.go.dev/encoding/xml, live test.)
- **CI feasibility for the containerized load test** — SP image is a public GHCR package, `ubuntu-latest` runners have Docker; pull-for-test path is feasible in public-repo Actions. (Source: `.github/workflows/build.yml`.)
