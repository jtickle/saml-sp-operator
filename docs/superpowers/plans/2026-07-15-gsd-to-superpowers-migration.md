# GSD → Superpowers Planning Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape this repo's planning from the GSD layout to the Superpowers layout — archive `.planning/`, add the process contract and SPInstance seed, and fix the one code comment the move breaks.

**Architecture:** A docs-only migration plus a single two-line code-comment repoint. All work lands **direct to `main`** as a one-time bootstrap (approach A from the design spec): the migration establishes the Superpowers process, so it cannot route through it, and it carries no runtime code change. Each task is one focused, independently-reviewable commit.

**Tech Stack:** git (`git mv` rename-preserving move), Markdown. No build, no runtime code, no tests — verification is by shell command with expected output.

**Design spec:** `docs/superpowers/specs/2026-07-15-gsd-to-superpowers-migration-design.md` (approved).

## Global Constraints

- **All commits land direct to `main`.** No feature branch, no PR for this migration (docs-only bootstrap, approach A).
- **One-time exception to "code reaches `main` only via PR":** Task 2 edits `internal/render/types.go` (comment-only, zero behavior change). Jeff granted this exception explicitly on 2026-07-15. It applies to Task 2 *only*; every future code change resumes the branch→PR rule.
- **Move with `git mv`** — never delete-and-recreate. History must survive `git log --follow`. This is NOT a history rewrite.
- **Public repo:** keep employer/infrastructure identifiers out of every commit, file, and message.
- **American English.** Banned words in authored prose: "simply", "just". Em-dashes are fine in Claude-authored prose.
- **Commit trailers** on every commit (repo convention):
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01WNUiWrM92FJ6s1wtqTrBK7
  ```
- **Starting state:** the design spec is already committed to `main` at `docs/superpowers/specs/2026-07-15-gsd-to-superpowers-migration-design.md` (commit `89559b4`), and this plan file lives at `docs/superpowers/plans/2026-07-15-gsd-to-superpowers-migration.md`. `docs/superpowers/specs/` and `docs/superpowers/plans/` therefore already exist — no separate "scaffold the directories" task is needed.

## File Structure

| Path | Action | Responsibility |
|---|---|---|
| `.planning/` → `docs/superpowers/archive/gsd-planning/` | Move (git mv) | Frozen historical GSD record |
| `docs/superpowers/archive/gsd-planning/README.md` | Create | ARCHIVED banner + retirement note |
| `internal/render/types.go:18-19` | Modify (comment) | Repoint the stale `.planning/` citation at `DESIGN.md` |
| `CLAUDE.md` (repo root) | Create | Project process contract (Superpowers workflow, slicing, branch model) |
| `docs/superpowers/specs/spinstance-controller-seed.md` | Create | Seed note for the first SPInstance brainstorm |

---

## Task 1: Archive the `.planning/` tree

**Files:**
- Move: `.planning/` (44 tracked files, incl. 13 tracked `research/.cache/*.json`) → `docs/superpowers/archive/gsd-planning/`
- Create: `docs/superpowers/archive/gsd-planning/README.md`

**Interfaces:**
- Consumes: nothing
- Produces: the archive path `docs/superpowers/archive/gsd-planning/` that Task 3 (CLAUDE.md) and Task 5 (verification) reference.

- [ ] **Step 1: Confirm the pre-move state**

Run: `git ls-files .planning | wc -l && test -d .planning && echo "PLANNING_PRESENT"`
Expected: `44` then `PLANNING_PRESENT`.

- [ ] **Step 2: Create the archive parent directory**

Run: `mkdir -p docs/superpowers/archive`
Expected: no output, exit 0.

- [ ] **Step 3: Move the tree with history-preserving `git mv`**

Run: `git mv .planning docs/superpowers/archive/gsd-planning`
Expected: no output, exit 0. (`git mv` on a directory moves all tracked files and stages the renames; the 13 `research/.cache/*.json` are tracked, so they move too.)

- [ ] **Step 4: Verify the move — old path gone, new path populated, renames staged**

Run: `test ! -e .planning && echo "ROOT_CLEAN"; git ls-files docs/superpowers/archive/gsd-planning | wc -l; git status --short | grep -c '^R'`
Expected: `ROOT_CLEAN`, then `44`, then a non-zero rename count (git may report some moves as delete+add rather than rename `R`; if the second number is `44` the move is complete regardless of how many are classified `R`).

- [ ] **Step 5: Write the archive README**

Create `docs/superpowers/archive/gsd-planning/README.md`:

```markdown
# ARCHIVED — GSD planning tree (historical, not maintained)

This directory is the repo's former `.planning/` tree, produced under the GSD
planning process. On 2026-07-15 the project migrated to the Superpowers process
(design spec: `docs/superpowers/specs/2026-07-15-gsd-to-superpowers-migration-design.md`).

**Status:** frozen reference. Nothing here is maintained or authoritative. Live
planning now lives in `docs/superpowers/` and the root `CLAUDE.md`.

**Still useful for:**

- Phase 3–6 requirement seeds — `REQUIREMENTS.md`, `ROADMAP.md` — for future slice brainstorms.
- The spike-learnings narrative — `threads/saml-sp-operator.md`.
- Phase 1's completed record and the `research/` bundle.

**Retirement:** safe to `git rm -r` this directory once the phase 3–6 seeds are
consumed. History is preserved either way; that is an ordinary delete, not a
history rewrite.
```

- [ ] **Step 6: Stage the README**

Run: `git add docs/superpowers/archive/gsd-planning/README.md`
Expected: no output, exit 0.

- [ ] **Step 7: Commit**

```bash
git commit -F - <<'EOF'
docs: archive GSD .planning tree under docs/superpowers/archive

Move the entire former GSD planning tree verbatim to
docs/superpowers/archive/gsd-planning/ via git mv (history preserved,
git log --follow intact). Add an ARCHIVED README marking it frozen and
non-authoritative, with a retirement note. Part of the GSD→Superpowers
migration (see docs/superpowers/specs/2026-07-15-gsd-to-superpowers-migration-design.md).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01WNUiWrM92FJ6s1wtqTrBK7
EOF
```

- [ ] **Step 8: Verify history survived the move**

Run: `git log --follow --oneline docs/superpowers/archive/gsd-planning/PROJECT.md | head -3`
Expected: at least one prior commit (e.g. the planning-trunk commit) shown under the old `.planning/PROJECT.md` path — proving `--follow` traces through the rename.

---

## Task 2: Repoint the `internal/render/types.go` comment

**Files:**
- Modify: `internal/render/types.go:18-19`

**Interfaces:**
- Consumes: the archive move from Task 1 (which is what made the old citation stale).
- Produces: nothing downstream.

**Context:** The move in Task 1 orphaned a two-line comment in committed Go source that cites `.planning/PROJECT.md` and a phase CONTEXT file. Repoint it at `DESIGN.md` (root, authoritative, durable) rather than the archive path — `DESIGN.md` carries the same render-core-is-k8s-free decision, and pointing at the archive would break again when the archive is later `git rm`'d. This is the one-time code-via-PR exception (comment-only).

- [ ] **Step 1: Read the current comment block for exact text**

Run: `sed -n '14,22p' internal/render/types.go`
Expected: shows the comment lines, including:
```
// without a base-container merge. See .planning/PROJECT.md Key Decisions and
// .planning/phases/01-shared-render-aggregation-package/01-CONTEXT.md (D-01,
```

- [ ] **Step 2: Edit the two lines**

Replace the two `.planning/…` references with a single `DESIGN.md` citation. Change:
```go
// without a base-container merge. See .planning/PROJECT.md Key Decisions and
// .planning/phases/01-shared-render-aggregation-package/01-CONTEXT.md (D-01,
```
to:
```go
// without a base-container merge. See DESIGN.md Key Decisions (the
// render-core-is-k8s-free decision, D-01,
```

(Preserve whatever text continues on the following line — the replacement keeps the sentence grammatical where it picks up on line 20. Read lines 18–21 first and adjust the tail so the sentence still reads correctly.)

- [ ] **Step 3: Verify no `.planning/` reference remains in the file**

Run: `grep -n '\.planning/' internal/render/types.go || echo "NO_PLANNING_REF"`
Expected: `NO_PLANNING_REF`.

- [ ] **Step 4: Verify the package still builds and vets (comment-only change must not perturb anything)**

Run: `go build ./internal/render/ && go vet ./internal/render/ && echo "BUILD_VET_OK"`
Expected: `BUILD_VET_OK`.

- [ ] **Step 5: Commit**

```bash
git add internal/render/types.go
git commit -F - <<'EOF'
docs(render): repoint types.go comment from .planning to DESIGN.md

The GSD→Superpowers migration archived .planning/, orphaning a two-line
doc comment in types.go that cited .planning/PROJECT.md and a phase
CONTEXT file. Repoint at DESIGN.md (root, authoritative, durable) — it
carries the same render-core-is-k8s-free decision and won't break when
the archive is later git rm'd. Comment-only; no behavior change.

One-time exception to the code-via-PR rule, granted by Jeff 2026-07-15
for this comment-only migration fallout. Future code changes resume
branch→PR.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01WNUiWrM92FJ6s1wtqTrBK7
EOF
```

---

## Task 3: Add the root `CLAUDE.md` process contract

**Files:**
- Create: `CLAUDE.md` (repo root)

**Interfaces:**
- Consumes: the archive path (Task 1) and the seed path (Task 4) it points at.
- Produces: the discoverable process contract a future session auto-loads.

**Context:** No project `CLAUDE.md` exists today. Root placement means Claude Code auto-loads it at session start (a nested one under `docs/superpowers/` would not load during root-level work). It overrides the global `~/.claude/CLAUDE.md` branch-topology default *for this repo* — sanctioned by the global file's own "project-specific overrides live in each project's own CLAUDE.md" rule.

- [ ] **Step 1: Write `CLAUDE.md`**

Create `CLAUDE.md` at the repo root:

```markdown
# saml-sp-operator — project instructions

This repo uses the **Superpowers** planning process. Global working
instructions live in `~/.claude/CLAUDE.md`; this file records only what is
specific to this repo, and it **overrides the global branch-topology default**
here (per the global file's "project-specific overrides live in each project's
own CLAUDE.md" rule).

## Planning process

- **Workflow:** brainstorm → spec (`docs/superpowers/specs/`) → writing-plans
  (`docs/superpowers/plans/`) → execute → finishing-a-development-branch. One
  slice at a time.
- **Authoritative design record:** `DESIGN.md` (§1–§12).
- **Former GSD planning tree:** archived, read-only, at
  `docs/superpowers/archive/gsd-planning/` (frozen; safe to delete once its
  phase 3–6 seeds are consumed).

## Slicing the v1 milestone

The remaining milestone — ship the v1 operator — is delivered as a
just-in-time sequence of independently-testable slices. **SPInstance
controller is first** (seed:
`docs/superpowers/specs/spinstance-controller-seed.md`). Each slice's
brainstorm decides the next boundary. **There is no committed roadmap.**

The five historical cuts are recorded as *non-binding intent* only, and may be
re-cut as work reveals better boundaries:

1. SPInstance controller (static path + production foundations)
2. AppIntegration controller (resolution only, no side effects)
3. Cross-namespace aggregation (SPInstance side)
4. AppIntegration Middleware emission, conflict & finalizer (end-to-end)
5. Hardening & observability closeout — may dissolve into earlier slices
   under verify-as-you-build

Requirement seeds for the later cuts live in the archive (`REQUIREMENTS.md`,
`ROADMAP.md`).

## Branch model

- **Planning-on-`main` is retired.** It was a GSD merge-conflict workaround;
  Superpowers' per-slice dated files do not collide the same way.
- **Per slice:** a worktree branch off `main`; spec + plan + code all commit on
  that branch and land in `main` via **one PR**.
  `finishing-a-development-branch` drives the merge.
- **Code reaches `main` only via PR** — the standing rule is unchanged and now
  covers planning docs too (they ride the slice branch).
- **Public repo:** keep employer/infrastructure identifiers out of every
  commit, planning doc, and message.
```

- [ ] **Step 2: Verify placement and the cross-references it makes**

Run: `test -f CLAUDE.md && grep -q 'spinstance-controller-seed.md' CLAUDE.md && grep -q 'archive/gsd-planning' CLAUDE.md && echo "CLAUDE_OK"`
Expected: `CLAUDE_OK`.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -F - <<'EOF'
docs: add project CLAUDE.md — Superpowers process contract

Record the repo-specific process: Superpowers workflow, just-in-time
slicing (SPInstance first, no committed roadmap), and the retired
planning-on-main branch model (unified branch + one PR per slice).
Overrides the global branch-topology default for this repo.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01WNUiWrM92FJ6s1wtqTrBK7
EOF
```

---

## Task 4: Write the SPInstance slice seed

**Files:**
- Create: `docs/superpowers/specs/spinstance-controller-seed.md`

**Interfaces:**
- Consumes: requirement content lifted from the archived `REQUIREMENTS.md` / `ROADMAP.md` / `STATE.md`.
- Produces: the starting note the first SPInstance brainstorm reads. **This task does NOT start that brainstorm.**

- [ ] **Step 1: Write the seed**

Create `docs/superpowers/specs/spinstance-controller-seed.md`:

```markdown
# SPInstance Controller — slice seed

**Not a spec.** A starting note for the first Superpowers brainstorm of the
SPInstance controller slice, lifted from the archived GSD roadmap/requirements
(`docs/superpowers/archive/gsd-planning/`). The brainstorm produces the dated
design spec; this only gives it a running start.

## Slice goal

A single `SPInstance` CR becomes a real, running, production-hardened SP
Deployment in the auth namespace — config-hash-gated rollout, real readiness,
memcached sessions, least-privilege secret access, network-isolated,
leader-elected — before any `AppIntegration` exists.

## Requirements to cover

- **SPI-01** — Reconcile an `SPInstance` into a running SP Deployment + Service
  + **headless** Service + ConfigMap.
- **SPI-02** — Roll the SP Deployment only when the config hash changes
  (pod-template annotation); unrelated reconciles don't churn the fleet.
- **SPI-03** — Readiness probe that proves `shibd` actually loaded (exercises a
  real handler), not a dumb nginx 200.
- **SPI-05** — Wire the memcached `Sessions`/`StorageService` when
  `sessionStore` is set.
- **SPI-07** — Fail-safe rollout: `RollingUpdate` with `maxUnavailable: 0`, so a
  config change whose new pod fails readiness can never retire a healthy pod
  (the ingress-nginx property). Pairs with SPI-02 (when to roll) and SPI-03
  (readiness proves shibd loaded).
- **OBS-03** — Surface the generated SP **metadata URL** in `SPInstance` status.
- **OBS-05** — Expose Prometheus metrics (controller-runtime defaults +
  reconcile/render/rollout counters).
- **SEC-01** — Generate a NetworkPolicy so the authenticator Service is
  reachable **only** from the gateway.
- **SEC-02** — Keep the SP private key isolated to the auth namespace (RBAC +
  informer-cache scoping).
- **SEC-03** — Reject invalid specs at admission via CRD validation (CEL /
  OpenAPI): malformed external URL, missing credentials, non-https/link-local
  metadata URL.
- **OPS-01** — Leader election enabled (single active reconciler across
  replicas).

## Cross-cutting foundations to front-load with this first controller

Leader election (OPS-01), base Prometheus metrics (OBS-05), Secret
RBAC/informer-cache scoped to the auth namespace only (SEC-02), NetworkPolicy as
an owned resource alongside the Deployment/Service/ConfigMap (SEC-01), and CRD
CEL validation for **both** CRDs (SEC-03) — cheapest to wire correctly now;
every later slice inherits them.

## Carried blockers / concerns to resolve during the brainstorm

- **SSRF-guard timing (SEC-04, later slice, but decide the seam now):** is the
  IdP metadata-URL guard admission-time CEL only (https + not-link-local, no
  network fetch) or does the operator itself fetch? A well-precedented CVE
  class — resolve explicitly, not by convention.
- **`shibd` reload-vs-restart:** v1 defaults to always-roll gated by config-hash
  (DESIGN §11). Hot-reload is a deferred v1.x optimization (OPS-03), not a v1
  blocker.
- **NetworkPolicy enforcement is CNI-specific:** "the YAML exists" ≠ "the control
  is enforced." Plan an actual in-cluster verification against this cluster's CNI
  (Calico), not just manifest generation.

## Grounding references

- `DESIGN.md` §5–§7, §9, §11 — CRD shapes, operator design, sessions, gateway
  attachment, header hygiene.
- `docs/superpowers/archive/gsd-planning/REQUIREMENTS.md` — full requirement
  wording.
- `docs/superpowers/archive/gsd-planning/threads/saml-sp-operator.md` — spike
  fixes M/N/O and the learnings-to-carry-into-the-operator section.
- `internal/render/` — the Phase 1 render package this controller drives.
```

- [ ] **Step 2: Verify the seed lists every intended requirement ID**

Run: `for id in SPI-01 SPI-02 SPI-03 SPI-05 SPI-07 OBS-03 OBS-05 SEC-01 SEC-02 SEC-03 OPS-01; do grep -q "$id" docs/superpowers/specs/spinstance-controller-seed.md || echo "MISSING $id"; done; echo "SEED_CHECK_DONE"`
Expected: `SEED_CHECK_DONE` with no `MISSING` lines above it.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/spinstance-controller-seed.md
git commit -F - <<'EOF'
docs: seed the SPInstance controller slice

Lift the SPInstance requirements, cross-cutting foundations, and carried
blockers from the archived GSD roadmap into a starting note for the first
Superpowers brainstorm. Seed only — does not start the brainstorm.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01WNUiWrM92FJ6s1wtqTrBK7
EOF
```

---

## Task 5: Final verification sweep (acceptance gate, no commit)

**Files:** none modified — this task only verifies the design spec's completion criteria.

**Interfaces:**
- Consumes: the results of Tasks 1–4.
- Produces: a go/no-go on the migration.

- [ ] **Step 1: `.planning/` is gone from the root; content present under the archive with history**

Run: `test ! -e .planning && echo "ROOT_CLEAN"; git ls-files docs/superpowers/archive/gsd-planning | wc -l; git log --follow --oneline docs/superpowers/archive/gsd-planning/ROADMAP.md | head -1`
Expected: `ROOT_CLEAN`, `44`, and one commit line (history traced through the rename).

- [ ] **Step 2: The Superpowers tree exists and holds the expected files**

Run: `ls docs/superpowers/specs/ docs/superpowers/plans/ && test -f CLAUDE.md && echo "TREE_OK"`
Expected: the specs dir shows `2026-07-15-gsd-to-superpowers-migration-design.md` and `spinstance-controller-seed.md`; the plans dir shows `2026-07-15-gsd-to-superpowers-migration.md`; then `TREE_OK`.

- [ ] **Step 3: No live `.planning/` path reference survives outside the archive**

Run: `grep -rn '\.planning/' --exclude-dir=.git . | grep -v '/docs/superpowers/' || echo "NO_STRAY_REFS"`
Expected: `NO_STRAY_REFS`. (Every legitimate remaining mention lives under `docs/superpowers/` — the migration spec/plan prose and the archive itself. The `internal/render/types.go` citation was repointed in Task 2, so it must NOT appear here. If anything else prints, it is a stray live reference and must be fixed before declaring the migration complete.)

- [ ] **Step 4: Confirm the four migration commits are on `main`**

Run: `git log --oneline -5`
Expected: the four migration commits (archive, types.go repoint, CLAUDE.md, seed) plus the earlier spec commit `89559b4`, all on `main`.

- [ ] **Step 5: Announce completion and the handoff**

The migration is complete. The next work session starts the SPInstance controller via `superpowers:brainstorming`, reading `docs/superpowers/specs/spinstance-controller-seed.md` and `DESIGN.md` — the first true dogfood of the new pipe on a real slice (worktree branch → spec + plan + code → one PR).

---

## Self-Review

**Spec coverage** (against `2026-07-15-gsd-to-superpowers-migration-design.md`):

- Archive `.planning/` verbatim via `git mv` → Task 1 ✓
- Archive README (banner + retirement note) → Task 1 Step 5 ✓
- Root `CLAUDE.md` process contract (workflow, slicing, branch model, archive pointer) → Task 3 ✓
- SPInstance seed (requirements + cross-cutting + blockers) → Task 4 ✓
- Direct-to-`main` bootstrap → Global Constraints + every task's commit ✓
- "No dangling references" → **corrected during planning**: the spec claimed all `.planning/` refs were inside `.planning/`; a `*.go` grep found `internal/render/types.go`. Fixed in Task 2 (one-time code-via-PR exception, approved). Verified clean in Task 5 Step 3. ✓
- Verification criteria (spec §Verification 1–5) → Task 5 Steps 1–5 map 1:1 ✓
- Scaffold `docs/superpowers/{specs,plans}/` → already exist (spec commit + this plan); folded into Global Constraints, no separate task ✓

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Full file contents are inline for every created file. ✓

**Type consistency:** No code interfaces beyond one comment edit; the archive path `docs/superpowers/archive/gsd-planning/` and seed path `docs/superpowers/specs/spinstance-controller-seed.md` are spelled identically across Tasks 1, 3, 4, and 5. ✓
