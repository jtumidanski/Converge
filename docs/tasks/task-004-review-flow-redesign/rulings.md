# task-004 — rulings made during execution

Extracted from the SDD ledger (`.superpowers/sdd/plan/progress.md`, git-ignored)
before the workspace was deleted. This is the durable record of every decision
taken on the maintainer's behalf while executing plan.md, with what each costs if
wrong. Sessions 1-3 covered tasks 1-27; session 4 was the final fix wave.

closure as `func(ctx, perPage, upstream int)`, which cannot be passed to
FilterWalk's actual `fetch func(context.Context, int) ([]T, bool, error)` (Task 2,
filterwalk.go). Implementer used two closures matching the existing ListRepositories
pattern in the same file. Ruling: accept the deviation — the brief's signature does
not compile, and the substitute follows the precedent Task 3 established one function
away. Cost if wrong: a shape difference from the plan text in one private closure;
behaviour is what the brief's tests assert. Review dispatched to confirm behaviour.
Task 4: complete (commit 0ff967e, review clean — 2 minors, both the ruled deviations, verified behaviourally equivalent)
Tasks 5-6: dispatched as one batch (task-implementer @ sonnet), BASE 0ff967e.
Tasks 5-6: implemented DONE_WITH_CONCERNS (commits 8cc884a, e3780d0). Concern: the
reordering branch of `defaultFirst` (api/branches.go:77-95) is never exercised —

---

components import `cn` from `@/lib/utils` and `components.json` declares
`"utils": "@/lib/utils"`. Verified `cn@0.2.6` is a real shadcn-ui package and a
drop-in clsx+tailwind-merge replacement, so nothing is broken — this is a scope and
consistency defect, not a correctness one. Ruling: normalise the generated files to
`@/lib/utils` and drop the `cn` package. Why: context.md states "New runtime
dependency: `cmdk` ... No others", components.json already names the utils alias, and
two idioms for one helper in one directory is drift that compounds across the 18 UI
tasks still to come. Cost if wrong: a future `npx shadcn add` regenerates with the
`cn` import and the normalisation must be repeated; the dependency is re-addable in
one command.
Kept without change: 3 extra generated files (dialog, input-group, textarea) are

---

the same review id" requirement) and clear `localStorage` directly in `afterEach`,
bypassing the store API. The plan's own Task 9 implementation therefore fails the
plan's own Task 10 tests: 4 of 29 assertions, measured, real stale-state failures.
Ruling: accept the implementer's fix — `get()` compares the raw stored string against
the last-seen raw string and re-parses only on change; `set()` primes `lastRaw` from
pre-write state so a throwing write still serves the intended value. Why: the tests
are the binding contract and the two plan sections cannot both stand; the public
`Store<T>` interface, `createStore` signature, and referential-stability guarantee are
all unchanged, so this is an internals correction, not a contract change. The
alternative — dropping the singletons — contradicts Task 10's explicit interface list.
Cost if wrong: `get()` does one extra `localStorage.getItem` per call (a string

---

"sorts a null merge time last". The sample computes a delta between merge instants and
gates on `Number.isFinite`; with a null mergedAt mapped to Infinity, `Infinity - finite`
is not finite, so the guard sends the comparison to the `number` tie-break instead of
sorting the null last. Ruling: accept the implementer's correction — compare instants
for inequality first, which also avoids the `Infinity - Infinity = NaN` unknown-vs-unknown
case, and fall through to `number` only on a true tie. Why: the test is the binding
contract, the Global Constraint states the rule plainly ("ascending mergedAt, then
ascending number; a null mergedAt sorts last"), and the sample contradicts both. Cost if
wrong: none identified — the corrected form satisfies the stated rule in every ordering
the brief's tests exercise, including the unknown-vs-unknown pair the sample produced
NaN for.

---


Ruling 9: The plan's Task 13 `useHotkeys` sample mutates a ref during render, which this
repo's `react-hooks/refs` ESLint rule rejects — the sample cannot pass `npm run lint`,
a required gate. Ruling: accept the implementer's move of the ref sync into its own
`useEffect`, preserving the "no resubscribe on rerender" behaviour the brief's tests
assert. Why: the lint gate is a branch-completion requirement in CLAUDE.md and the
sample's pattern is a genuine React correctness smell, not merely a style preference.
Cost if wrong: the handler ref updates one effect-flush later than the sample intended;
the brief's tests cover the rerender-without-resubscribe case that this could affect.

Ruling 10: The plan's Task 14 `isValidRepositoryName` sample fails its own test —
`atlas/.hidden` is asserted to reject but the sample accepts it, because it rejects only
an exact "." segment. Ruling: accept the implementer's correction — reject any path
segment beginning with "." or "-". Why: the test is the binding contract, and the
broader rule is what the test's intent (no hidden or option-like segments reaching git)
requires. Cost if wrong: a repository whose path segment legitimately starts with "-"
or "." is rejected client-side; both are already invalid or hostile inputs for the git
argument positions these names reach.
Tasks 13-14: review dispatched (sonnet) over f382410..f1cb6d0.
Tasks 11-12: fix round 1/5 dispatched (fresh task-implementer @ sonnet) for F1.

---

Ruling 13: The plan's Task 16 breadcrumb design uses ONE combined context; the
continuation implementer found that shape causes an infinite re-render loop reachable by
the brief's OWN Publisher test — measured, not theorised: the test hung indefinitely
before the fix and passes 6/6 in 1.4s after. Ruling: accept the split into a stable
setter context (`useBreadcrumbs`) plus a segments context (`Breadcrumbs`). Why: the
brief's own test is the binding contract and the single-context shape cannot pass it; the
public `useBreadcrumbs` / `BreadcrumbSegment` contract that Tasks 18, 22 and 26 consume
is unchanged, so no downstream task is affected. Cost if wrong: one extra context
provider in the shell; the consumer-facing API is identical either way.

OPERATIONAL FINDING (explains the earlier stall): vitest's default `threads` pool hangs

---

validator rejects makes `resolveTyped` silently no-op, because `parseRepositoryInput`
returns null and nothing handles null. No error, no toast, no message — the user types a
real repository, presses enter, and the UI does nothing at all.
Ruling: the SILENT NO-OP is a defect independent of whether the validator should be
stricter than the backend, and it is in scope for the drawer that Task 18 owns. Fix it in
Task 18's fix round: surface a strings.ts-routed inline error when parseRepositoryInput
returns null, matching the existing repositoryNotFound treatment. Do NOT change the
validator or its tests — Ruling 12 still governs that, and it remains the user's call.
Why: "invalid input produces no feedback whatsoever" is a defect under any reading of the
spec, and fixing it costs one branch in one handler. Cost if wrong: an error message
appears for input that previously failed silently — strictly more information.

---

Correction: HEAD at Task 21 dispatch was 814d2b3 (docs commit, child of b762b57), not
  b762b57. Review BASE for Task 21 = 814d2b3.
Task 21: implementer returned DONE_WITH_CONCERNS (commit 9542f57). Two brief conflicts:
18. Ruling: ACCEPT the implementer's fix to the brief's own sample ChangeRow.onRowClick.
    The brief excluded only <a> targets from the row toggle, so clicking the row's own
    checkbox fired both the checkbox handler and the row handler -> net no-op, making the
    checkbox dead. Widened to closest("a, button"). The spec requires a working selection
    affordance; the brief's sample contradicts the spec and loses. Mutation-verified
    (revert fails 3 tests). Cost if wrong: row-click stops toggling when the click
    originates on a nested button; additive to narrow later.
19. Ruling: ACCEPT the minimal adaptation of SelectChangesPage.tsx (+ its test), which is
    outside the brief's Files list but is the only other ChangeTable consumer. Leaving it
    would break tsc/build. Task 22 is expected to replace it. Cost if wrong: throwaway
    edit to a file Task 22 rewrites.
Note: new react-refresh/only-export-components warning on ChangeTable.tsx is inherent to
  the brief's mandated co-export of buildRows + ChangeTable. Not a defect to fix here.
Task 21: review dispatched (sonnet) over 814d2b3..9542f57, carrying rulings 18/19 as
  settled-but-verify, the performed-mutation rule, the shared-value fixture trap, the

---

  strengthen the brief's single-item SelectionBar fixture to two items before the
  remove-button mutation discriminated -- the shared-value trap again, caught by the
  implementer this time. Two brief conflicts resolved:
20. Ruling: ACCEPT adding local MSW beforeAll/afterEach/afterAll to the brief's
    SelectChangesPage.test.tsx sample, which omitted them. Repo has no global
    server.listen(); without them the tests pass for the wrong reason. Cost if wrong: none.
21. Ruling: ACCEPT fixing the brief's final assertion to unwrap the JSON:API envelope
    ({data:{attributes:{...}}}) that reviewsService.create actually posts, rather than
    changing the established service contract to match the brief's bare
    `createdBody.changes`. The service contract is pre-existing and outside this task's
    scope; the brief's assertion was simply wrong about it. Cost if wrong: the assertion
    tests the envelope shape rather than a flatter one the plan may have intended --
    no production behavior affected.
CONTROLLER CONCERN for the Task 22 review: the reported suite total is UNCHANGED at

---

    observes resulting selection state (stronger than mock-arg assertion); mutation
    (!== select -> === select) fails the test. Ruling 21's envelope-unwrap assertion
    verified as a real check, not weakened. Tree clean.
22. Ruling: BACKFILL the coverage dropped by check A in this same fix round, rather than
    deferring. The behaviors are still live in the page; a brief that replaces a test file
    1:1 while dropping cases is exactly the silent-regression shape this plan has been
    burned by four times. The brief's authority covers what to BUILD, not permission to
    delete existing coverage of untouched behavior. Cost if wrong: one extra fix round and
    a slightly larger test file than the brief specifies.
Task 22: fix round 1/5 dispatched -- resumed the original implementer with both items.
NOTE: SendMessage is DISABLED in this session -- live subagents cannot be resumed. Per the

---

        would silently drop on every keystroke and nothing notices.
    (c) ChangeTable loading prop hardcoded false -> 362/362 pass. ChangeTable.test.tsx
        never renders loading={true} at all.
23. Ruling: FIX ALL THREE in fix round 2 rather than deferring as out-of-scope. They are
    not incidental pre-existing gaps -- they were deleted by the SAME brief step that
    Ruling 22 already judged, and (b) in particular is a live behavior of this very page
    with zero coverage, where the regression is silent and user-visible. The implementer
    asserted equivalent coverage existed; it does not, and an unsupported coverage claim
    is the precise failure mode this plan has been burned by. Cost if wrong: one extra
    fix round for three tests.
Task 22: fix round 2/5 dispatched -- fresh implementer (SendMessage disabled).

---

Task 23: dispatched implementer (sonnet, task-implementer) at BASE ae4e58b.
Task 23: implementer returned DONE (commit 823e867). 44 files/372 tests, tsc clean,
  lint 0 errors. 4/4 mutations with quoted failures.
24. Ruling: ACCEPT splitting `#number` and title into sibling spans in
    IncludedChangesPopover. The brief's own sample combined them into ONE text node, which
    cannot satisfy the brief's OWN assertion getByText("ATLAS-7 add a thing"). The brief
    contradicts itself; the assertion is the more specific expression of intent and wins.
    Cost if wrong: markup has one extra span than the plan pictured; purely presentational.
CONTROLLER CONCERN for Task 23 review: the status badge renders "Ready" unconditionally,
  regardless of the review's actual status -- transcribed verbatim from the brief's sample
  and untested. Reviewer must judge whether Task 23's own brief makes this correct

---

  ReviewStatusLine ONLY inside case "READY" of an exhaustive switch guarded by
  assertUnreachable(status: never) -- a real compile-time enforcement. But Task 26 is not
  implemented yet, so today nothing enforces it.
25. Ruling: LEAVE the hardcoded "Ready" badge in Task 23 and make Task 26 responsible for
    landing the exhaustive switch. Adding status handling to the component would duplicate
    the page-level switch the plan already designs and widen Task 23 past its brief.
    Cost if wrong: if Task 26's switch does not land as designed, a non-READY review
    renders a false "Ready" badge. MUST VERIFY IN TASK 26 -- this is a blocking check on
    that task, not a nicety.
Task 23: minor (deferred): the brief's own ReviewStatusLine fixture never exercises
  "second included change carries the ticket key, first does not", so reversing the

---

Task 24: implementer returned DONE (commit 6fb6578). 44 files/378 tests, tsc clean, lint
  0 errors. 7/7 mutations caught with quoted failures, performed in a scratch copy outside
  the worktree.
26. Ruling: ACCEPT using node.path (not node.name) for the directory row label/aria-label.
    The brief's sample used node.name but its OWN tests expect "src/main/java/com/atlas"
    and /collapse src\/test/i, which only node.path can produce (name omits the ancestor
    "src", which has 2 children and so does not collapse into the chain). Third
    self-contradictory brief on this plan; the assertion wins again. Cost if wrong: the
    directory row shows the full path where the plan may have pictured a leaf name.
27. Ruling: ACCEPT replacing the brief's ancestor-expanding useEffect with this repo's
    existing adjust-state-during-render pattern (as in useSelection.ts). eslint's
    react-hooks/set-state-in-effect REJECTS the brief's version outright -- the plan text
    cannot be implemented as written without a lint error, and the repo already has a
    sanctioned idiom for it. Cost if wrong: a behavioral difference in WHEN ancestors
    expand; the reviewer is tasked with mutation-checking the expansion behavior.
28. Ruling: ACCEPT the minimal ReviewPage.tsx / ReviewPage.test.tsx compat update (new
    required viewed/onToggleViewed props, role button->treeitem) although both are outside
    Task 24's Files list. Without it the gate does not compile. Scaffolding (EMPTY_VIEWED,
    no-op toggle) is explicitly Task 26's to replace. Same shape as Ruling 19. Cost if
    wrong: throwaway edit to a file Task 26 rewrites.
DEFECT FOUND BY CONTROLLER, ATTRIBUTED TO TASK 23: `npx prettier --check` FAILS on
  apps/frontend/src/components/features/review/__tests__/ReviewStatusLine.test.tsx.
  Verified directly by the controller. Task 23's implementer did not run format:check and

---

  rows at one depth. This is the exact "fixture too small" pattern, now on its 8th task.
  format:check CONFIRMED: Task 24's own six files all pass prettier; exactly one file
  repo-wide fails -- ReviewStatusLine.test.tsx from Task 23.
29. Ruling: TREAT BOTH Important findings as loop-entering despite the reviewer calling
    them non-blocking. The skill's loop triggers on any Important finding, and both are
    proven-by-mutation coverage holes on behavior this task exists to deliver -- exactly
    what Task 22's two extra rounds taught. Also fold in the Task 23 format:check fix, a
    one-command CI-breaking defect nobody else owns. Cost if wrong: one fix round.
Task 24: minor (deferred): ARIA tree pattern is partial -- role="tree" is set but directory
  rows are plain buttons (no role="treeitem"), no role="group" nesting, no aria-level /
  aria-posinset / aria-setsize. Inherited verbatim from the brief's sample, not

---

  0 errors, format:check clean. NOTABLE: the implementer self-caught a gap the brief's own
  8 tests left unreached (DiffPane error banner, 0/1 caught) and added a test for it --
  the first time on this plan an implementer found its own uncovered path before review.
30. Ruling: ACCEPT routing "Path copied" / "Open in {provider}" / "File i of n" / the
    error-banner title through strings.ts instead of the brief's bare literals. Standing
    Global Constraint; twelfth task to need it. Cost if wrong: none.
31. Ruling: ACCEPT the one extra test beyond the brief's eight (DiffPane error branch).
    Not YAGNI -- it covers a failure path the brief specified in prose but left unreached
    by its own test list. Cost if wrong: one extra test.
CONTROLLER CONCERN for the Task 25 review: only 3 mutations were performed on a large
  diff-rendering component. The acute risk for a diff pane is line-number/off-by-one and
  line-type classification, which the implementer did NOT mutate. Reviewer must mutate
  those specifically. Also: FileHeader's no-slash-path branch (cut === -1) is exercised
  only implicitly, never directly asserted -- implementer flagged it, reviewer must judge.

---

  lost.
  IMPORTANT FINDING: FileHeader's STATUS_LETTER / STATUS_COLOR mapping is completely
  unasserted -- swapping the modified and deleted letters leaves the full suite green.
32. Ruling: FIX the STATUS_LETTER/STATUS_COLOR gap in a fix round rather than deferring,
    despite the reviewer calling it non-blocking. It is a one-line test, it is this task's
    own classification logic, and Task 26 wires this pane in next -- cheaper now than after.
    Cost if wrong: one cheap fix round.
Task 25: fix round 1/5 dispatched (haiku -- single-file mechanical test addition).
Task 25: fix round 1 implemented (commit 6e022ea). 393 tests, all four gates clean.
  Claims both STATUS_LETTER and STATUS_COLOR swaps caught (2/393 fail each).
Task 25: fix round 1 scoped re-review dispatched (haiku) over 4abed8a..6e022ea.

---

  gates clean. Obligation 1 (exhaustive switch + assertUnreachable) reported landed
  UNMODIFIED; obligation 2 scaffolding (EMPTY_VIEWED, no-op onToggleViewed, bare
  setExplicitPath) reported fully replaced. Only 3 mutations performed.
33. Ruling: ACCEPT hoisting changeNumbers.join(",") into a named changeNumbersKey variable.
    The brief's inline .join() inside the useMemo dep array fails this repo's
    react-hooks/exhaustive-deps "simple expressions only" check even with a disable
    comment -- the brief cannot be implemented as written. Fourth brief that cannot be
    transcribed literally. Cost if wrong: none, identical semantics.
CONTROLLER CONCERNS for the Task 26 review -- three required checks:
  (a) OBLIGATION 3 UNADDRESSED IN THE REPORT: the converge.viewed.<reviewId> read must be
      TOTAL (missing / malformed / wrong-shape / throwing storage all -> default). The

---

Task 26: fix round 1 implemented (commit 730cb44). 402/402, tsc -b --force clean (the REAL
  check), lint 0 errors, format clean. Claims Critical regression test discriminates and
  the isPaths mutation is caught 1/402.
34. Ruling: ACCEPT lifting `confirming` to a required CONTROLLED prop on ReviewStatusLine
    (with ReviewStatusLine.test.tsx's renderLine becoming a ControlledReviewStatusLine
    wrapper). Lifting the dialog state is the only way the parent can gate hotkeys on it,
    and it matches the repo's two existing precedents (SelectChangesPage sheetOpen,
    ReviewsPage popoverOpen). Cost if wrong: ReviewStatusLine is no longer usable
    uncontrolled; it has exactly one consumer, so the blast radius is nil.
Task 26: fix round 1 scoped re-review dispatched (sonnet) over 9edfb68..730cb44 -- told to
  re-run both mutations AND to check that the controlled-prop refactor did not weaken

---

   (d) messageFor(error, fallback) prefers the server message, so a JSON:API 500
       never reaches fallback copy -- test uses HttpResponse.error(). Reachability
       trap worth remembering.
  Ruling: accepted the #1 deviation (baseName helper vs inline split) -- behaviour is
   identical and it serves finding #5's dedup; re-reviewer asked to judge equivalence
   and check for broken callers. Cost if wrong: a one-line revert to the inline form.
Scoped re-review dispatched (opus, frontend-guidelines-reviewer) over e8f47f1..e30a7e8.
  Package: .superpowers/sdd/plan/review-e8f47f1..e30a7e8.diff
  Report:  .superpowers/sdd/plan/final-fix-wave-re-review.md
  Briefed to re-perform its OWN mutations (report evidence not trusted), to scrutinise
  the three new tokens for light/dark + registration, and to judge the surviving-then-
