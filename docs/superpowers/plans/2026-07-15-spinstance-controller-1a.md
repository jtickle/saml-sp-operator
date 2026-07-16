# SPInstance Controller 1a Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Applying one `SPInstance` produces a running, host-agnostic Shibboleth SP (Deployment + ClusterIP Service + headless Service + ConfigMap), with config-hash-gated rollout, a fail-safe `maxUnavailable:0` rollout, and a real shibd-loaded readiness probe.

**Architecture:** First revise the shared `internal/render` package to emit host-agnostic config (relative `handlerURL`, no single `ExternalURL`), then build the `SPInstanceReconciler` on top of it. Render stays k8s-free; the controller adapts the CRD spec into `render.SPConfig`, renders the three config files, hashes them, and reconciles four owned objects.

**Tech Stack:** Go, kubebuilder/controller-runtime, envtest, testcontainers-go (the proven `shibdload` container harness).

**Spec:** `docs/superpowers/specs/2026-07-15-spinstance-controller-1a-design.md`.

## Global Constraints

- **Multi-host is the model.** The SP renders host-agnostic: relative `handlerURL` (`/Shibboleth.sso`), `SHIBSP_SERVER_SCHEME=https` as pod env only, nginx uses the per-request `$host`. Verified by `internal/render/shibmultihost_test.go`.
- **No `SPConfig.ExternalURL`** and no `SPInstanceSpec.externalURL` — removed/never added.
- **Readiness probe (pinned empirically):** exec `curl -fsS http://localhost:8080/Shibboleth.sso/Status` (curl is in the image; wget is not). No nginx.conf change needed.
- **SP image:** operator-global flag `--sp-image`, default the pinned digest `ghcr.io/jtickle/saml-sp-operator/shib-authenticator@sha256:0e33ee7fea4524cb3caa8744b22f05a80703d22444ef198368484dc523f41319` (same pin as `shibdload_test.go`).
- **Container mount contract** (from the proven `shibdload` harness): rendered files → `/etc/shibboleth/shibboleth2.xml`, `/etc/shibboleth/attribute-map.xml`, `/etc/nginx/nginx.conf`; credential Secret → `/run/shibboleth/sp-credentials/tls.{key,crt}`; IdP-metadata backing at `render.shibMetadataProviderBackingFilePath` (needs a writable volume). Confirm exact writable-dir needs in Task C1's container test.
- **Ownership:** all four objects `ownerRef` the `SPInstance` (same namespace). No finalizer in 1a.
- **Deferred (do NOT implement):** memcached, NetworkPolicy, RBAC/cache scoping, CEL, leader election, metrics, OBS-03 metadata URL, `boundCount`. Those are 1b/later.
- **Branch/commits:** this worktree (`feat/spinstance-controller`); everything lands via the slice PR. American English; no "simply"/"just". Commit trailers:
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01WNUiWrM92FJ6s1wtqTrBK7
  ```
- **envtest:** set `KUBEBUILDER_ASSETS=/home/claude/saml-sp-operator/bin/k8s/1.36.2-linux-amd64` (the worktree shell doesn't inherit it).

## File Structure

| Path | Action | Responsibility |
|---|---|---|
| `internal/render/selfurl.go` | Modify | Relative handler; drop absolute-host derivation |
| `internal/render/shibboleth2.go` | Modify | Relative `handlerURL`; stop consuming `ExternalURL` |
| `internal/render/nginxconf.go` | Modify | Per-request `$host`, `:443`, no `ExternalURL` |
| `internal/render/types.go` | Modify | Remove `SPConfig.ExternalURL` |
| `internal/render/*_test.go`, `testdata/golden/*` | Modify | New host-agnostic golden output |
| `internal/render/shibmultihost_test.go` | Modify | Drive the revised `RenderShibboleth2` |
| `internal/controller/spinstance_config.go` | Create | `SPInstance` → `render.SPConfig` + render + hash |
| `internal/controller/spinstance_objects.go` | Create | Build ConfigMap/Deployment/Services |
| `internal/controller/spinstance_controller.go` | Modify | Reconcile + status + SetupWithManager |
| `internal/controller/spinstance_controller_test.go` | Modify | envtest reconcile tests |
| `cmd/main.go` | Modify | `--sp-image` flag → reconciler field |

---

# Phase A — Render revision (host-agnostic)

## Task A1: Relative handlerURL + remove `SPConfig.ExternalURL`

**Files:**
- Modify: `internal/render/types.go` (remove `ExternalURL` from `SPConfig`), `internal/render/selfurl.go`, `internal/render/shibboleth2.go`, `internal/render/nginxconf.go`
- Test: `internal/render/selfurl_test.go`, `internal/render/shibboleth2_test.go`, `internal/render/nginxconf_test.go`

**Interfaces:**
- Produces: `RenderShibboleth2(cfg SPConfig, winners []AppBinding)` now emits `handlerURL="/Shibboleth.sso"`; `RenderNginxConf(cfg SPConfig)` emits a `$host`-based responder with `SERVER_PORT 443`, no external-port pin. `SPConfig` no longer has `ExternalURL`. `DeriveSelfURL` is removed (no remaining caller).
- Consumes: `AppBinding.{Scheme,Port}` remains the source for per-app `<Host>` scheme/port (unchanged, RENDER-05).

- [ ] **Step 1: Update the shibboleth2 test to expect a relative handlerURL**

In `internal/render/shibboleth2_test.go`, change the expected `handlerURL` assertion (and any golden reference) from the absolute `https://host:port/Shibboleth.sso` to `/Shibboleth.sso`. Example assertion:

```go
if !strings.Contains(string(out), `handlerURL="/Shibboleth.sso"`) {
    t.Errorf("expected relative handlerURL; got:\n%s", out)
}
if strings.Contains(string(out), "https://") && strings.Contains(string(out), `handlerURL="https://`) {
    t.Errorf("handlerURL must not be absolute/host-pinned (multi-host):\n%s", out)
}
```

- [ ] **Step 2: Run it, watch it fail**

Run: `go test ./internal/render/ -run TestRenderShibboleth2 -v`
Expected: FAIL (still emits absolute handlerURL).

- [ ] **Step 3: Make `handlerURL` relative in `shibboleth2.go`**

In `RenderShibboleth2`, replace the `DeriveSelfURL(cfg.ExternalURL)` call and `HandlerURL: selfURL.HandlerURL` with the constant relative handler:

```go
// handlerURL is RELATIVE so one SP spans multiple app hosts: the SP
// reconstructs the per-request host itself (verified: shibmultihost_test.go).
// SHIBSP_SERVER_SCHEME=https (pod env) forces the scheme; no host is pinned.
const relativeHandlerURL = "/Shibboleth.sso"
```
Set `HandlerURL: relativeHandlerURL` on the Sessions element and delete the `selfURL` derivation and its error path from this function.

- [ ] **Step 4: Remove `ExternalURL` from `SPConfig` and fix `nginxconf.go`**

In `types.go` delete the `ExternalURL string` field and its doc. In `nginxconf.go`, delete the `DeriveSelfURL(cfg.ExternalURL)` call; the responder block uses the per-request host and standard port (mirror `shibmultihost_test.go`'s `multiHostNginxConf`): `SERVER_NAME $host`, `HTTP_HOST $host`, `SERVER_PORT 443`, `HTTPS on`. Delete `selfurl.go`'s `DeriveSelfURL`/`SelfURL` (no callers remain) — confirm with `grep -rn 'DeriveSelfURL\|SelfURL\|\.ExternalURL' internal/`.

- [ ] **Step 5: Fix all compile sites and run the package build**

Run: `grep -rn 'ExternalURL' internal/ ` → update every setter (`fixtures_test.go`, `shibdload_test.go`, `nginxconf_test.go`, `selfurl_test.go`). Delete `selfurl_test.go`'s `DeriveSelfURL` cases (function gone).
Run: `go build ./internal/render/ && go vet ./internal/render/`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add internal/render/
git commit -F - <<'EOF'
feat(render): host-agnostic self-URL for multi-host SPs (RENDER-02)

Emit a relative handlerURL (/Shibboleth.sso) and stop deriving a single
pinned SHIBSP_SERVER_NAME/PORT + absolute handlerURL from one ExternalURL.
One SP now spans multiple app hosts: the SP reconstructs the per-request
host, SHIBSP_SERVER_SCHEME=https (pod env) forces the scheme. Removes
SPConfig.ExternalURL and DeriveSelfURL (no remaining consumer). Verified
by shibmultihost_test.go.
<trailers>
EOF
```

## Task A2: Refresh golden fixtures + adapt the multi-host regression test

**Files:**
- Modify: `internal/render/testdata/golden/shibboleth2.xml`, `internal/render/testdata/golden/nginx.conf` (whatever the golden set is), `internal/render/shibmultihost_test.go`

- [ ] **Step 1: Regenerate/hand-edit the golden files** to the new relative-handler output. If the package has a golden-update mode (`-update` flag), run `go test ./internal/render/ -run TestRenderShibboleth2 -update`; otherwise edit the golden `handlerURL` and nginx responder block by hand to match Task A1's output.

- [ ] **Step 2: Adapt `shibmultihost_test.go` to drive the real renderer.** Replace the hand-crafted `multiHostShibboleth2XML()` with a call to `RenderShibboleth2(cfg, winners)` where `winners` are two `AppBinding`s on `appa.example.com` and `appb.example.com`; keep `multiHostNginxConf` (or switch to `RenderNginxConf(cfg)`). The per-host ACS assertions stay.

- [ ] **Step 3: Run the full render suite + the container tests**

Run: `go test ./internal/render/...` (hermetic)
Run: `go test -tags shibdload ./internal/render/... -run 'TestShibdLoad|TestMultiHostSelfURL' -v`
Expected: all pass — shibd loads the revised config AND the multi-host ACS check still passes against the real renderer.

- [ ] **Step 4: Commit** (`feat(render): golden fixtures + multi-host regression on revised renderer`).

---

# Phase B — SPInstance controller

## Task B1: `SPInstance` → `render.SPConfig` + render + hash

**Files:**
- Create: `internal/controller/spinstance_config.go`, `internal/controller/spinstance_config_test.go`

**Interfaces:**
- Produces: `func renderConfig(sp *samlv1alpha1.SPInstance) (files map[string]string, hash string, err error)` returning `{"shibboleth2.xml":..., "attribute-map.xml":..., "nginx.conf":...}` and the `render.Hash` over them. Credential mount paths are the fixed container paths (Global Constraints).
- Consumes: the revised `render` API (`RenderShibboleth2(cfg, nil)`, `RenderNginxConf(cfg)`, `RenderAttributeMap(nil)`, `Hash`).

- [ ] **Step 1: Write the test** (`spinstance_config_test.go`): a hand-built `SPInstance` → `renderConfig` returns three non-empty files, a stable hash, and the shibboleth2.xml contains the spec's `entityID` and a relative `handlerURL`. A second call with an unrelated metadata-field change produces a different hash; a no-op change produces the same hash.

```go
func TestRenderConfig(t *testing.T) {
    sp := newSampleSPInstance() // entityID, idp.metadataURL, credentials.name
    files, h1, err := renderConfig(sp)
    if err != nil { t.Fatal(err) }
    if !strings.Contains(files["shibboleth2.xml"], sp.Spec.EntityID) { t.Error("entityID missing") }
    if !strings.Contains(files["shibboleth2.xml"], `handlerURL="/Shibboleth.sso"`) { t.Error("handler not relative") }
    _, h2, _ := renderConfig(sp)
    if h1 != h2 { t.Error("hash not stable for identical spec") }
}
```

- [ ] **Step 2: Run → fail** (function undefined). `go test ./internal/controller/ -run TestRenderConfig`.

- [ ] **Step 3: Implement `renderConfig`** mapping `SPInstanceSpec` → `render.SPConfig` (EntityID, IdP{MetadataURL, EntityID}, credential paths = `/run/shibboleth/sp-credentials/tls.key|crt`, in-memory `SessionDefaults` from the spike values — lifetime 28800/timeout 3600/relayState "ss:mem"/checkAddress false/handlerSSL true/cookieProps "https", `RemoteUser: ["email","uid"]` as the 1a default). Render the three files (empty winners/attrs — no apps in 1a), `render.Hash` over them.

- [ ] **Step 4: Run → pass. Commit.**

## Task B2: ConfigMap builder + reconcile

**Files:** `internal/controller/spinstance_objects.go` (create), `spinstance_controller.go` (wire), envtest in `spinstance_controller_test.go`.

**Interfaces:** `func (r *SPInstanceReconciler) reconcileConfigMap(ctx, sp, files map[string]string) (*corev1.ConfigMap, error)` — create-or-update, `ownerRef` set, name `sp.Name+"-sp"`.

- [ ] **Step 1: envtest** — apply an `SPInstance` (+ its credentials Secret), reconcile, assert a ConfigMap `<name>-sp` exists with the three rendered keys and an ownerRef to the SPInstance.
- [ ] **Step 2: Run → fail.**
- [ ] **Step 3: Implement** using `controllerutil.CreateOrUpdate` + `ctrl.SetControllerReference`. Data = the three files.
- [ ] **Step 4: Run → pass. Commit.**

## Task B3: Deployment builder (rollout gating + fail-safe + readiness)

**Files:** `spinstance_objects.go`, `spinstance_controller.go`, envtest.

**Interfaces:** `func (r *SPInstanceReconciler) reconcileDeployment(ctx, sp, configHash string) (*appsv1.Deployment, error)`.

- [ ] **Step 1: envtest** — after reconcile, assert a Deployment `<name>-sp` exists with: `Spec.Strategy.RollingUpdate.MaxUnavailable == 0`; a pod-template annotation `saml.tickletechnologies.com/config-hash == configHash`; container image == `r.SPImage`; env `SHIBSP_SERVER_SCHEME=https`; a readiness `Exec` probe running `curl -fsS http://localhost:8080/Shibboleth.sso/Status`; volume mounts for the ConfigMap (at `/etc/shibboleth` + `/etc/nginx/nginx.conf` subpath) and the credential Secret (at `/run/shibboleth/sp-credentials`); an `emptyDir` at `/run/shibboleth`. Then reconcile again with the SAME spec → assert the pod-template annotation (hence the Deployment generation) is unchanged (SPI-02). Change `entityID` → assert the annotation changes.
- [ ] **Step 2: Run → fail.**
- [ ] **Step 3: Implement** the Deployment spec. Key fields, spelled out:

```go
maxUnavailable := intstr.FromInt(0)
dep.Spec.Strategy = appsv1.DeploymentStrategy{
    Type: appsv1.RollingUpdateDeploymentStrategyType,
    RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: &maxUnavailable},
}
dep.Spec.Template.Annotations = map[string]string{"saml.tickletechnologies.com/config-hash": configHash}
// container:
c := corev1.Container{
    Name:  "sp",
    Image: r.SPImage,
    Env:   []corev1.EnvVar{{Name: "SHIBSP_SERVER_SCHEME", Value: "https"}},
    Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
    ReadinessProbe: &corev1.Probe{
        ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{
            Command: []string{"curl", "-fsS", "http://localhost:8080/Shibboleth.sso/Status"},
        }},
        InitialDelaySeconds: 5, PeriodSeconds: 10, FailureThreshold: 3,
    },
    VolumeMounts: []corev1.VolumeMount{
        {Name: "shib-config", MountPath: "/etc/shibboleth/shibboleth2.xml", SubPath: "shibboleth2.xml"},
        {Name: "shib-config", MountPath: "/etc/shibboleth/attribute-map.xml", SubPath: "attribute-map.xml"},
        {Name: "shib-config", MountPath: "/etc/nginx/nginx.conf", SubPath: "nginx.conf"},
        {Name: "sp-credentials", MountPath: "/run/shibboleth/sp-credentials", ReadOnly: true},
        {Name: "shib-run", MountPath: "/run/shibboleth"},
    },
}
```
Volumes: `shib-config` from the ConfigMap `<name>-sp`; `sp-credentials` from `sp.Spec.Credentials.Name`; `shib-run` an `emptyDir`. Set `ownerRef`. Note: the credential mount + `/run/shibboleth` writable dir + subPath layout must be **confirmed against the real container** in Task C1 (the `shibdload` harness proves the file locations; the writable-dir need is inferred).

- [ ] **Step 4: Run → pass. Commit.**

## Task B4: ClusterIP + headless Services

**Files:** `spinstance_objects.go`, envtest.

- [ ] **Step 1: envtest** — assert a ClusterIP Service `<name>-sp` and a headless Service `<name>-sp-headless` (`ClusterIP: "None"`), both selecting the pod labels, port 8080, ownerRef'd.
- [ ] **Step 2: Run → fail. Step 3: Implement. Step 4: pass. Commit.**

## Task B5: Status (conditions, hash, observedGeneration, Degraded)

**Files:** `spinstance_controller.go`, envtest.

- [ ] **Step 1: envtest** — after a successful reconcile: `status.configHash == <hash>`, `observedGeneration == generation`, `ConfigRendered=True`, and `Ready` reflects Deployment availability. With the credentials Secret ABSENT: `Degraded=True` with a human-readable reason and no panic.
- [ ] **Step 2: Run → fail.**
- [ ] **Step 3: Implement** the reconcile ordering: Secret-existence check first (missing → set `Degraded`, return without creating objects); else render → reconcile objects → set `ConfigRendered`/`configHash`/`observedGeneration`; set `Ready` from the Deployment's `Available` condition via `meta.SetStatusCondition` + status patch.
- [ ] **Step 4: Run → pass. Commit.**

## Task B6: `--sp-image` flag

**Files:** `cmd/main.go`, `spinstance_controller.go` (add `SPImage string` field).

- [ ] **Step 1:** Add `SPImage string` to `SPInstanceReconciler`. In `main.go`, `flag.StringVar(&spImage, "sp-image", defaultSPImage, "...")` with `defaultSPImage` = the pinned digest; pass it into the reconciler at `SetupWithManager`.
- [ ] **Step 2:** `go build ./... && go vet ./...`. Commit.

---

# Phase C — Verification

## Task C1: Container-confirm the running SP + full suite

**Files:** possibly a new `internal/controller` or `internal/render` `shibdload`-tagged test, or a manual kind run.

- [ ] **Step 1: Confirm the Deployment's container contract against the real image.** Using the `shibdload` harness pattern, boot the pinned image with the exact mounts the Deployment specifies (ConfigMap subPaths, credential path, `/run/shibboleth` emptyDir) + `SHIBSP_SERVER_SCHEME=https`, and assert shibd reaches `"Shibboleth initialization complete."` and the readiness command `curl -fsS http://localhost:8080/Shibboleth.sso/Status` exits 0. This closes the inferred mount/writable-dir details from Task B3 with real evidence.
- [ ] **Step 2: Full hermetic + container suites green.**

Run: `KUBEBUILDER_ASSETS=/home/claude/saml-sp-operator/bin/k8s/1.36.2-linux-amd64 go test ./...`
Run: `go test -tags shibdload ./internal/render/... -v`
Expected: all pass.

- [ ] **Step 3 (human/live):** Jeff applies an `SPInstance` (+ a credentials Secret) to a real cluster/kind and confirms a running, Ready SP pod whose `/Shibboleth.sso/Status` is healthy — the one thing envtest cannot prove (it runs no shibd). Report results; fix inline only if it's an obvious plan-scope defect, else todo.

---

## Self-Review

**Spec coverage:**
- SPI-01 (four objects) → B2/B3/B4 ✓
- SPI-02 (hash-gated rollout) → B1 (hash) + B3 (annotation, no-op vs change tests) ✓
- SPI-03 (real readiness) → B3 (curl `/Status` exec probe, empirically pinned) + C1 ✓
- SPI-07 (`maxUnavailable:0`) → B3 ✓
- RENDER-02 host-agnostic revision → A1/A2 ✓
- Status/Degraded → B5 ✓; `--sp-image` → B6 ✓
- Deferred items (memcached/netpol/RBAC/CEL/leader/metrics/OBS-03) → correctly absent ✓

**Placeholder scan:** the only "confirm against real container" items (B3 mounts, C1) are backed by a concrete test method + the proven harness, not hand-waves. Readiness command is exact. No TBD.

**Type consistency:** `renderConfig` returns `(map[string]string, string, error)` and is consumed by B2 (files) and B3/B5 (hash) consistently; `r.SPImage` set in B6, used in B3; object name `<name>-sp` consistent across B2–B4.
