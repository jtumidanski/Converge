---
description: Phase 4 — invoke superpowers:subagent-driven-development to implement a planned task in its existing worktree
argument-hint: Task identifier — accepts "task-001-bucket-replication", "task-001", "001", or "1"
---

You are starting Phase 4 of the Converge four-phase development workflow. Argument: **$ARGUMENTS**

## Process

### Step 1 — Resolve the task

Same fuzzy-match algorithm as `/design-task` Step 1:

1. Glob `docs/tasks/task-*` (main) and `.worktrees/*/docs/tasks/task-*` (sibling worktrees).
2. Match `$ARGUMENTS` against folder names — exact, number-only (`1`/`001`/`task-1`/`task-001`), or slug fragment.
3. Zero matches → ask for correction. Multiple matches → list and let the user pick.
4. If the task lives only on main with no worktree, stop and tell the user the task needs a worktree.
5. Resolve to `<worktree>/docs/tasks/<id>/`.

### Step 2 — Ensure we're in the right worktree

Run `pwd`. If it does NOT match `<worktree>`, `cd <worktree>` yourself and continue from there. Do NOT ask the user to re-run the command — per CLAUDE.md's "Worktree Discipline" rule, cd into the task worktree yourself.

Do NOT create a new worktree — the worktree was created by `/spec-task` and must be reused so phase artifacts stay co-located.

### Step 3 — Validate inputs

Confirm `<worktree>/docs/tasks/<id>/plan.md` AND `context.md` exist. If either is missing, tell the user to complete `/plan-task` first.

### Step 4 — Invoke subagent-driven-development

Use the Skill tool to invoke `superpowers:subagent-driven-development` (default). Pass:

- Plan path: `<worktree>/docs/tasks/<id>/plan.md`
- Context path: `<worktree>/docs/tasks/<id>/context.md`
- Project conventions: `<worktree>/CLAUDE.md`
- **Worktree absolute path** (`<worktree>`) for every dispatched implementer subagent. Subagent prompts MUST enforce cwd-discipline — every Bash call prefixed with `cd <worktree> && ...`, post-commit branch verification, no destructive git ops, no `git add -A` / `git add .`.

If the user explicitly requests inline mode this session (rare), invoke `superpowers:executing-plans` instead.

### Step 5 — On completion

After all plan tasks complete and verify, the chosen skill hands off to `superpowers:finishing-a-development-branch`. Honor that handoff. Then suggest:

> All plan tasks complete. Recommend running `superpowers:requesting-code-review` next, which dispatches the plan-adherence-reviewer agent (plus any guideline reviewers once they're defined).

## Important Rules

- The worktree was created by `/spec-task`. NEVER create a new one here.
- Never start implementation outside the task worktree.
- Follow plan steps exactly; stop and ask when blocked rather than guessing.
- Run the verification commands the plan specifies; don't claim completion based on assumption.
