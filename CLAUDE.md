# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Converge is a combined PR/MR review tool: one place to review pull requests and merge requests across hosting providers. The system is a **Go** backend plus a **React/TypeScript** web UI, laid out as `apps/backend` and `apps/frontend`. The repository is currently unscaffolded — only `README.md` and this tooling exist. Module structure, frameworks, and build tooling will be decided during the first task; update this file (build commands, service paths, conventions) once those are settled.

## Workflow Rules

When asked to understand or plan something, DO NOT start implementing code changes. Wait for explicit approval before making any edits. Planning and implementation are separate phases.

## Build & Verification

A branch is "done" only when all of these are clean. Commands are provisional until the first task lands tooling; correct them here when that happens.

**Backend** (cwd = `apps/backend`):
- `go test -race -count=1 ./...`
- `go vet ./...`
- `golangci-lint run`
- `CGO_ENABLED=0 go build ./...`

**Frontend** (cwd = `apps/frontend`):
- `npm ci`
- `npm run lint`
- `npm run format`
- `npm test`
- `npm run build`

Node is not always on `PATH` — if `npm` is missing, load it first:

```sh
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
```

## Code Patterns

When refactoring shared types or creating common libraries, prefer straightforward moves over re-exporting type aliases. Keep abstractions clean — don't break service boundaries by having one layer call another's internals directly.

## Development Workflow

The canonical flow for any non-trivial change is four phases. **`/spec-task` creates a dedicated worktree at `.worktrees/task-NNN-slug/` on a `task-NNN-slug` branch; all subsequent phases run inside that worktree** so docs, code, and the eventual PR are one unit. Each phase is a separate slash command, invoked from a fresh (`/clear`'d) session so the next phase consumes only the prior phase's documented artifacts:

1. `/spec-task <idea>` — run from the main repo. Interactive PRD interview that creates the worktree + branch and commits the PRD. Output: `<worktree>/docs/tasks/task-NNN-slug/prd.md`.
2. `cd .worktrees/task-NNN-slug`, `/clear`, then `/design-task <task-id>` — invokes `superpowers:brainstorming`. Output: `design.md` (committed on the task branch).
3. `/clear`, then `/plan-task <task-id>` — invokes `superpowers:writing-plans`. Output: `plan.md` + `context.md` (committed).
4. `/clear`, then `/execute-task <task-id>` — invokes `superpowers:subagent-driven-development`. Reuses the existing worktree; never creates a new one.

Phase commands accept fuzzy task identifiers: `task-001-slug`, `task-001`, `001`, or `1` all resolve to the same folder. They search both `docs/tasks/` (main) and `.worktrees/*/docs/tasks/` to locate the task.

Task numbers are assigned by `tools/task-numbers.sh next` (single source of truth). A SessionStart hook runs `tools/task-numbers.sh check` and surfaces any task-number collisions before they compound.

Skip `/spec-task` only for trivial fixes that don't warrant a PRD; document those directly via a brainstorming session.

### Artifact Location Override

Both `superpowers:brainstorming` and `superpowers:writing-plans` default to `docs/superpowers/specs/` and `docs/superpowers/plans/`. **In this project, both go under `docs/tasks/task-NNN-slug/` instead.** When invoking those skills directly (outside the phase commands), pass the task folder explicitly so artifacts land in the right place.

### Code Review Pattern

Code review uses three modular reviewer agents, dispatched in parallel:

- `plan-adherence-reviewer` — verifies plan tasks were actually implemented
- `backend-guidelines-reviewer` — Go DOM-* / SUB-* / SEC-* checklist (when Go files changed)
- `frontend-guidelines-reviewer` — React/TS FE-* checklist (when frontend files changed)

Invoke via `superpowers:requesting-code-review` (it dispatches the appropriate subset), or invoke an individual agent directly for ad-hoc checks. Each agent writes findings to `docs/tasks/task-NNN-slug/audit.md`.

The backend and frontend reviewer checklists are sourced from the `backend-dev-guidelines` and `frontend-dev-guidelines` skills in `.claude/skills/`. The `skill-activation-prompt` hook (wired in `.claude/settings.json`) auto-suggests those skills based on file/intent triggers configured in `.claude/skills/skill-rules.json`.

## Design/Plan Output Style

- When producing design.md or plan.md documents, write the full document directly to the file. Do NOT walk through sections interactively or ask for per-section approval. The user will read the committed file.

## Worktree Discipline

- Tasks live in git worktrees (siblings of the main repo under `.worktrees/`). Before planning/designing/executing a task, verify cwd is the correct worktree; if not, `cd` into it yourself rather than asking the user.
- When searching for task PRDs/plans/designs, search across all worktrees (`git worktree list`) before concluding a file is missing.
- Never edit files in the main repo when a task worktree exists for that work.
- Never hardcode absolute home-directory paths in docs, commands, or scripts. Resolve the repo root with `git rev-parse` / `$CLAUDE_PROJECT_DIR` and build paths from there.

## Code Review Before PR

- Always run the code-review step (`/audit-plan` or `superpowers:requesting-code-review`) before opening a PR. Do not skip even when the task plan looks complete.

## Verification Over Memory

- For GitHub/GitLab API contracts, configuration values, and service-to-service interactions, verify against local source or upstream provider docs rather than citing values from memory or general knowledge.
- When uncertain about behavior, read the source rather than speculating.

## Context & Cost Guards

This repo opts in to the guards in `~/.claude/hooks/` via `.claude/guards.json`. They are enforced by hooks, not by honor system — several will refuse a tool call outright. Full reference: `~/.claude/hooks/README.md`.

- **Hand off past ~150k context.** The controller never carries a session past ~60 tool calls (≈150k tokens) into a *new* unit of work — unconditionally, no carve-out for tasks remaining. Write the diagnosis into `docs/tasks/task-NNN-slug/`, then have the user `/clear` and re-run the phase command; it resumes from the committed artifacts. Finishing the unit in flight (reviewers, verifiers, doc agents) is still allowed.
- **Subagents stop at 120 tool calls.** Commit what works and report PARTIAL with what is done file by file, what remains, and the exact next step. PARTIAL at the cap is the contracted outcome, not a failure — the controller dispatches a continuation with fresh context.
- **Brief a fresh agent; do not fork.** A fork re-reads this entire conversation on every turn. Dispatch a named agent type with an explicit brief instead; when sharding a review, give each child the artifact path plus its own scope.
- **Never spend a turn waiting.** No `sleep`, no `ps aux`/`pgrep` polling, no re-reading a log until it changes. Use `run_in_background: true` and let the completion notify you, or `Monitor` with an `until` loop and an explicit timeout.
- **No absolute home paths under `docs/`.** Write repo-relative paths.

Each of these has a one-line escape hatch when the exception is real: `CONTEXT-JUSTIFIED:`, `FORK-JUSTIFIED:`, or `POLL-JUSTIFIED:` followed by the reason, anywhere in the prompt or command.
