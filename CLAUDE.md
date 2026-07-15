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
