# GSD → Superpowers Planning Migration — Design

**Date:** 2026-07-15
**Status:** Approved (design); pending implementation plan
**Author:** Claude (brainstormed with Jeff)

## Goal

Migrate this repo's planning from the GSD process to the Superpowers process.
Retire GSD's living-tracking machinery, adopt Superpowers' brainstorm → spec →
plan → execute flow, and retire the planning-on-`main` branch split that GSD's
merge-conflict pressure had forced.

## Background

The repo has been planned with GSD since project init. GSD centralizes state in
a few large, continuously-mutated files (`.planning/STATE.md`,
`.planning/ROADMAP.md`, `.planning/PROJECT.md`, `.planning/REQUIREMENTS.md`) plus
a per-phase artifact set. Because those shared files churn merge conflicts when
planning rides feature branches, the project adopted a workaround: **all planning
committed to `main`, code on `gsd/phase-NN` branches via PR.**

Superpowers works differently. Its unit is a **per-feature dated spec/plan file**
(`docs/superpowers/specs/`, `docs/superpowers/plans/`) that two efforts rarely
touch at once. The shared-file conflict pressure that justified planning-on-`main`
therefore largely evaporates — which is what makes retiring the split safe.

**Current position:** Phase 1 (shared render/aggregation package) is complete and
merged (PR #1). The next real work is the SPInstance controller — GSD's "Phase 2".

## Decisions

Four decisions were resolved during the brainstorm:

1. **Fully Superpowers.** Drop GSD's living tracking (ROADMAP / REQUIREMENTS /
   STATE as maintained artifacts). Requirements survive as prose inside each
   slice's spec.
2. **Archive `.planning/` in-tree**, non-authoritative, now; ordinary `git rm`
   later when it is spent (history retains it regardless).
3. **Unified branch, one PR, worktree-isolated** per slice. Planning-on-`main` is
   retired: a slice's spec + plan + code all travel on one branch and land in
   `main` via a single PR.
4. **No committed roadmap.** The remaining milestone (ship v1 operator) is
   delivered as a just-in-time sequence of independently-testable slices,
   **SPInstance first** as the migration's proof. Slice boundaries are decided
   one at a time; the historical five-cut sequence is recorded only as
   non-binding intent.

A fifth, process-mechanics decision: **the migration itself commits direct to
`main`** as a one-time bootstrap (approach A) — it is pure docs (no code, so the
"code reaches `main` only via PR" rule is not triggered) and it *establishes* the
process, so it cannot route through it. SPInstance becomes the first true dogfood
of the unified-branch + PR model.

## Target state

### Directory layout

```
CLAUDE.md                                     ← project process contract (root, auto-discovered; § below)
docs/superpowers/
  specs/    YYYY-MM-DD-<topic>-design.md       ← brainstorm output
            spinstance-controller-seed.md      ← SPInstance slice seed (not a dated spec)
  plans/    YYYY-MM-DD-<feature>.md            ← writing-plans output
  archive/
    gsd-planning/                              ← the entire current .planning/ tree, moved verbatim
      README.md                                ← ARCHIVED banner + retirement note
```

The process contract lives at **root `CLAUDE.md`** (not under `docs/superpowers/`)
so Claude Code auto-loads it when a session starts at the repo root — precisely
when a future agent needs to discover that this repo uses Superpowers. It
complements, and does not duplicate, the global `~/.claude/CLAUDE.md`.

`.planning/` moves verbatim via `git mv .planning docs/superpowers/archive/gsd-planning`
— one move, full history preserved, working tree clean, every artifact still
readable for future slice brainstorms.

### What replaces GSD's moving parts

| GSD artifact | Fate | Superpowers replacement |
|---|---|---|
| `STATE.md` (current position) | Archived | Git branch + latest dated spec/plan *is* the state |
| `ROADMAP.md` (fixed phase list) | Archived | **Nothing** — slices decided just-in-time |
| `REQUIREMENTS.md` + traceability | Archived | Requirements as prose inside each slice's spec |
| `PROJECT.md` | Archived | `DESIGN.md` (authoritative decision record) + project `CLAUDE.md` |
| per-phase CONTEXT / RESEARCH / PATTERNS / VALIDATION / VERIFICATION | Archived | spec (design) + plan (tasks) + TDD / verification-before-completion skills |
| `threads/saml-sp-operator.md` | Archived | No equivalent; durable content overlaps `DESIGN.md`; archived as-is |
| `research/` bundle | Archived | Reference; re-read when a slice needs it |
| `config.json` | Archived | N/A — Superpowers is skill-driven, not config-driven |

### Project process contract (root `CLAUDE.md`)

No project-level `CLAUDE.md` exists today; process rules live only in the global
file and the (now-archived) GSD tree. The migration creates a short root
`CLAUDE.md` recording the new contract:

- **Workflow:** brainstorm → spec (`docs/superpowers/specs/`) → writing-plans
  (`docs/superpowers/plans/`) → execute → finishing-a-development-branch. One
  slice at a time.
- **Slicing:** the milestone (ship v1 operator) is a just-in-time sequence of
  independently-testable slices — SPInstance controller first. Each slice's
  brainstorm decides the next boundary. **No committed roadmap.** The five
  historical cuts (SPInstance / AppIntegration-resolution / cross-namespace
  aggregation / Middleware+conflict+finalizer / hardening) are recorded as
  *non-binding intent*, seeded from the archive; hardening may dissolve into
  earlier slices under verify-as-you-build.
- **Branch model:** planning-on-`main` is **retired**. Per slice: a worktree
  branch off `main`; spec + plan + code all commit there and land via **one PR**
  into `main`. `finishing-a-development-branch` drives the merge. The standing
  rule (code reaches `main` only via PR) is unchanged and now covers planning
  too.
- **Pointer** to `docs/superpowers/archive/gsd-planning/` for historical context
  and phase 3–6 requirement seeds.

### SPInstance slice seed

A starting note the first SPInstance brainstorm consumes, lifting from the
archived roadmap/requirements:

- **Requirements:** SPI-01, SPI-02, SPI-03, SPI-05, SPI-07, OBS-03, OBS-05,
  SEC-01, SEC-02, SEC-03, OPS-01.
- **Cross-cutting front-load:** leader election, RBAC / informer-cache scoping to
  the auth namespace, base Prometheus metrics, NetworkPolicy as an owned
  resource, CRD CEL validation for both CRDs.
- **Carried blockers/concerns** (from `STATE.md`): SSRF-guard timing (admission
  CEL vs. operator fetch), `shibd` reload-vs-restart classification (v1 defaults
  to always-roll gated by config-hash), and Calico NetworkPolicy *enforcement*
  test (manifest existence ≠ control enforced).

Landing spot: `docs/superpowers/specs/spinstance-controller-seed.md` — an
undated seed note the SPInstance brainstorm reads, distinct from the dated
SPInstance design spec that brainstorm will later produce. **Not started this
session.**

## Migration mechanics

The migration dogfoods the Superpowers flow on itself: this brainstorm → **this
spec** → writing-plans → execute. All commits land **direct to `main`** (approach
A).

Ordered steps (detailed task breakdown belongs to the writing-plans output):

1. `git mv .planning docs/superpowers/archive/gsd-planning`.
2. Add `docs/superpowers/archive/gsd-planning/README.md` (ARCHIVED banner +
   "safe to `git rm` once phase 3–6 seeds are consumed").
3. Scaffold `docs/superpowers/specs/` and `docs/superpowers/plans/` (this spec is
   the first file in `specs/`).
4. Write root `CLAUDE.md` (the process contract above).
5. Write the SPInstance slice seed to
   `docs/superpowers/specs/spinstance-controller-seed.md`.
6. Commit to `main`.

### No dangling references

Verified: `AGENTS.md` contains zero GSD/planning/phase/roadmap references, and no
project `CLAUDE.md` exists. Every GSD / `.planning` mention in the repo lives
*inside* `.planning/` itself. Archiving the directory leaves no broken pointers
elsewhere in the tree.

### GSD tooling

GSD skills remain installed globally; the migration simply stops using them for
this repo. No uninstall is in scope.

## Scope boundary (this session)

This session produces the **migration spec only** (this document). No files move,
nothing is archived, no `CLAUDE.md` or seed is written, no code changes. Execution
happens later off the plan that writing-plans generates.

## Non-goals

- Building the SPInstance controller (a separate slice, kicked off after
  migration execution).
- Uninstalling or modifying GSD tooling.
- Re-slicing the remaining milestone differently than the five natural cuts
  (confirmed to stand).
- Rewriting git history to remove `.planning/` (ordinary `git rm` later, not a
  history rewrite).

## Verification

The migration is proven complete when:

1. `.planning/` no longer exists at the repo root; its content is present under
   `docs/superpowers/archive/gsd-planning/` with history intact
   (`git log --follow`).
2. `docs/superpowers/{specs,plans}/` exist; `specs/` holds this spec and the
   SPInstance seed.
3. Root `CLAUDE.md` records the process contract.
4. No file outside the archive references GSD or `.planning/`.
5. The next work session can start the SPInstance controller via
   `superpowers:brainstorming` reading only `docs/superpowers/` — the first true
   dogfood of the new pipe.
