# Task 27 — verification sweep audit

Recorded gaps found while walking PRD §10 acceptance criteria against the
test suite. No UI browser is available in this sandbox, so all criteria
were checked against Vitest/Go test evidence rather than a running UI.

## Gap: drawer "Cancel" and scrim-click close are not directly asserted

PRD §10: `"Start a new review" row and the n key open the drawer; Escape,
Cancel, and scrim close it.`

`apps/frontend/src/pages/__tests__/ReviewsPage.test.tsx` covers:
- opening via the "start a new review" row (line 100)
- opening via `n` and closing via `Escape` (line 107)
- recent-repositories listing with an empty query (line 119)
- server search + selection (line 133)
- inline error on an unresolvable pasted `owner/name` (line 147)
- inline error on a malformed typed value (line 169)

No test in the repo asserts that clicking the drawer's "Cancel" button or
the scrim (overlay) also closes it. This is very likely inherited,
well-tested behaviour from the underlying shadcn/Radix `Sheet` primitive
(Escape and overlay-click are wired through the same `onOpenChange`
handler as the explicit close/cancel actions), but it is not independently
asserted in this codebase, so it cannot be marked confirmed from test
evidence alone.

Files: `apps/frontend/src/components/features/newReview/NewReviewSheet.tsx`,
`apps/frontend/src/pages/__tests__/ReviewsPage.test.tsx`

No component-level test file exists for `NewReviewSheet.tsx`,
`ProviderSelect.tsx`, `RepositoryResults.tsx`, or `RepositorySearch.tsx`
individually — their behaviour is exercised only indirectly through
`ReviewsPage.test.tsx`. That is sufficient to reach the behaviour (drawer
open/close, search, paste-resolve, error paths are all reached), but the
Cancel-button and scrim-click paths specifically are not reached by any
test, direct or indirect.

## All other §10 boxes

Confirmed via test evidence (see task-27-report.md for the full mapping):
routes/header/breadcrumbs, root page listing, backend repository search
filter, backend branches endpoint, base-branch select, hide-bots filter,
group-by-ticket, author chips, selection bar + apply order, review page
(tree/header/folds/shortcuts), viewed-state persistence, localStorage
persistence with malformed-value tolerance, and the five `make` gates
(all run and PASSED in this sweep).
