# Resume Active Reviews — UX Flow

Approved layout and interaction decisions from the spec interview. These are the
reference renderings for the design phase; they are illustrative of structure and
information hierarchy, not pixel-exact.

## Root page (`/`) with active sessions

```
┌──────────────────────────────────────────────────────┐
│ Combined Review                                      │
│ Choose a provider and a repository to start.         │
├──────────────────────────────────────────────────────┤
│ Resume a review                                (3)   │
│                                                      │
│  ● READY      GitLab Work · atlas/server             │
│    #12, #14, #15 · 5 files · +84 −12                 │
│    started 12m ago · expires in 5h                   │
│                              [Resume] [Discard]      │
│                                                      │
│  ◐ CREATING   GitLab Work · atlas/server             │
│    #21 · Resolving PRs/MRs                           │
│    started 40s ago · expires in 24h                  │
│                              [Resume] [Discard]      │
│                                                      │
│  ✕ FAILED     GitHub · web/ui                        │
│    #3 · CONFLICT on #3                               │
│    started 23h ago · expires in 47m       ← warning  │
│                              [Resume] [Discard]      │
├──────────────────────────────────────────────────────┤
│ Provider   [GitLab Work                    ▾]        │
│ Manual repository form…                              │
│ Repository list…                                     │
│ Pagination…                                          │
└──────────────────────────────────────────────────────┘
```

## Root page with no active sessions

The section is always rendered — it does not disappear when empty.

```
┌──────────────────────────────────────────────────────┐
│ Combined Review                                      │
│ Choose a provider and a repository to start.         │
├──────────────────────────────────────────────────────┤
│ Resume a review                                      │
│  ┌────────────────────────────────────────────────┐  │
│  ┊   No reviews in progress.                      ┊  │
│  ┊   Start one by choosing a repository below.    ┊  │
│  └────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────┤
│ Provider   [GitLab Work                    ▾]        │
└──────────────────────────────────────────────────────┘
```

## Discard confirmation

Discard is irreversible server-side (the worktree is deleted), so it is always
two-step. Cancelling issues no request.

```
│  ● READY      GitLab Work · atlas/server             │
│    #12, #14, #15 · 5 files · +84 −12                 │
│    Discard this review? This cannot be undone.       │
│                              [Cancel] [Discard]      │
```

## Status vocabulary

| Status | Badge | Row detail line | Resume |
|---|---|---|---|
| `READY` | default | totals (`N files · +A −D`) | yes |
| `CREATING` | secondary, in-progress | stage label via shared `stageLabel` | yes |
| `CONFLICTED` | destructive | error code, change number when present | yes |
| `FAILED` | destructive | error code, change number when present | yes |
| unknown | neutral/outline | raw status string | yes |

## Decision log

| Decision | Chosen | Rejected alternatives |
|---|---|---|
| Placement | Section on the root page above the provider picker | Dedicated `/reviews` route; persistent header banner on every page |
| Statuses shown | All four active states in one list | Only `CREATING`/`READY`; failures split into a separate "Needs attention" group |
| Refresh | Poll 2 s while any row is `CREATING`, then stop | Refetch on mount/focus only; unconditional 5 s poll |
| Row actions | Resume + Discard with confirmation | Resume only; Discard without confirmation |
| Empty state | Always render the section with an empty message | Hide the section entirely when empty |
| Row detail | Status, provider/repo, changes, totals, created + expiry | Minimal one-line row; full detail with base branch/SHA and change titles |
| Backend | No changes | Add `?status=` filter and `limit`; defer the decision to design |
