# task-004 — final fix wave re-review (scoped)

Branch `task-004-review-flow-redesign`, fix range `e8f47f1..e30a7e8` (one commit,
13 files, +142/-49). Scope: verdict the 7 findings, flag NEW breakage in the fix
diff only. Untouched-code observations are deferred minors.

All mutation evidence below was produced by me in this worktree against the FULL
suite, then reverted. The implementer's claimed mutation output was NOT trusted.

## Gates (run by me, cwd `apps/frontend`, Node v22.22.2)

| Command | Result |
|---|---|
| `npm run lint` | `✖ 2 problems (0 errors, 2 warnings)` — the two known pre-existing `react-refresh` warnings (`ChangeTable.tsx:29`, `RepositoryResults.tsx:21`) |
| `npm run format:check` | `All matched files use Prettier code style!` |
| `npx vitest run --pool=forks` | `Test Files 45 passed (45)` / `Tests 407 passed (407)` |
| `npx tsc -b --force` | exit 0, no output |
| `npm run build` | built; only the pre-existing chunk-size warning. Deleted `apps/backend/internal/ui/dist/.gitkeep`, restored with `git checkout --` |

## Per-finding verdicts

### 1. FR-36 bare filename — ADDRESSED

Fix: `apps/frontend/src/pages/ReviewPage.tsx:222`
`nextName={nextPath === undefined ? undefined : baseName(nextPath)}`, with
`baseName` imported at `:26`. `FileFooter.tsx:20` interpolates `nextName` raw, so
the page is the right place.

Test: `apps/frontend/src/pages/__tests__/ReviewPage.test.tsx:181-200`.

Fixture distinctness (the plan's signature defect): the override supplies
`src/main/app.ts` and `src/lib/nested/helper.ts` — different directories,
different depths (2 vs 3), different basenames. Neither input shares a value with
the other, and neither has path == name, so the assertion cannot pass by
coincidence. This is exactly the gap in `DiffPane.test.tsx`'s root-level footer
fixtures.

**My mutation** — `ReviewPage.tsx:222` reverted to `nextName={nextPath}`, full
suite:

```
 × labels Next file with the bare filename, not the path (FR-36) 331ms
 FAIL  src/pages/__tests__/ReviewPage.test.tsx > ReviewPage (READY) > labels Next file with the bare filename, not the path (FR-36)
Expected element to have accessible name:
  StringNotContaining "src/main/app.ts"
Received:
  Next file: src/main/app.tsj
 Test Files  1 failed | 44 passed (45)
      Tests  1 failed | 406 passed (407)
```

Exactly one test failed; the other 406 passed, so the mutant compiled and the
test is specific. Restored → 407/407.

**Deviation judgement (`baseName` reuse vs inline `split("/").pop()`):**
acceptable and equivalent. The moved body at `lib/review/fileTree.ts:115-118`
is byte-identical to the one deleted from `FileTreeRow.tsx`
(`lastIndexOf("/")`, `-1 → path`). It agrees with `split("/").pop()` on every
input including `""` and trailing-slash paths, and returns `string` rather than
`string | undefined`, which is stricter. Callers: `FileTreeRow.tsx:26` and
`ReviewPage.tsx:222` (grep across `src/` shows no others). No caller was broken —
`tsc -b --force` is clean and the tree-row tests still pass.

### 2. FE-06 design tokens — ADDRESSED

- `components/features/review/fileStatus.ts:14-19` — `text-warning` /
  `text-success` / `text-destructive` / `text-info`; consumed by
  `FileHeader.tsx:4` and `FileTreeRow.tsx:2`.
- `components/features/reviews/ReviewRow.tsx:12-13` — `bg-success`, `bg-warning`.
- No raw palette class remains in the three cited files.

**My mutation** — swapped `modified`/`added` in the shared
`STATUS_COLOR`, full suite:

```
 FAIL  DiffPane.test.tsx > DiffPane > status badge > displays 'M' in the warning colour for modified files
Error: expect(element).toHaveClass("text-warning")
    116|       expect(statusBadge).toHaveClass("text-warning");
 FAIL  DiffPane.test.tsx > DiffPane > status badge > displays 'A' in the success colour for added files
Error: expect(element).toHaveClass("text-success")
    160|       expect(statusBadge).toHaveClass("text-success");
 Test Files  1 failed | 44 passed (45)
      Tests  2 failed | 405 passed (407)
```

The failing tests render `FileHeader`, which proves that consumer now reads the
shared map. Restored → 407/407.

**The three NEW tokens — reviewed, and they are correct.**

- Both blocks are defined: `src/index.css:24-26` (`:root`) and `:112-114`
  (`.dark`). Neither mode is missing.
- They are registered the same way every other token is: `src/index.css:72-74`
  adds `--color-success`/`--color-warning`/`--color-info` inside `@theme inline`,
  immediately after `--color-destructive: var(--destructive)` (`:71`). A token
  defined but not registered would render as nothing; these are registered.
- Naming matches the file's convention (bare semantic name, `--color-*` mirror).
  `--destructive` has no `--destructive-foreground` sibling in this file
  (grep: zero matches), so the absence of `--success-foreground` etc. is
  consistent, not an omission.
- Values are the `--destructive` 600-light/400-dark convention exactly, verified
  by me against `node_modules/tailwindcss/theme.css`: light `--success`
  `oklch(0.627 0.194 149.214)` = green-600 (`theme.css:76`), `--warning`
  `0.666 0.179 58.318` = amber-600 (`:40`), `--info` `0.546 0.245 262.881` =
  blue-600 (`:136`); dark values = green-400 (`:74`), amber-400 (`:38`),
  blue-400 (`:134`). `--destructive` is red-600 (`:16`) light / red-400 (`:14`)
  dark. Exact match to the stated convention.
- **They actually emit CSS** (my own build, not the report's):

```
.bg-success{background-color:var(--success)}
.bg-warning{background-color:var(--warning)}
.text-info{color:var(--info)}
.text-success{color:var(--success)}
.text-warning{color:var(--warning)}
--info:oklch(54.6% .245 262.881);  --info:oklch(70.7% .165 254.624);
--success:oklch(62.7% .194 149.214);  --success:oklch(79.2% .209 151.711);
--warning:oklch(66.6% .179 58.318);  --warning:oklch(82.8% .189 84.429);
```

  Both the utility classes and both light/dark custom-property blocks are in the
  shipped stylesheet.
- Contrast is plausible and strictly better than what it replaced: light mode
  moves from the 500 shade to the darker 600 (amber L 0.769 → 0.666), dark mode
  from 500 to the lighter 400 (→ 0.828). Every change moves away from the
  background. `--warning` in light mode (amber-600, L 0.666) is still the weakest
  of the three against `--background` L 1.0; it is used only on a single-letter
  status glyph and on a 2×2 status dot, and it matches the convention the file
  already set with `--destructive`. Non-blocking.

### 3. False `useBranches` comment — ADDRESSED

`SelectChangesPage.tsx:83-90`. I verified the new comment against source rather
than accepting it: `BaseBranchSelect.tsx:47-52` passes `open` as the `enabled`
argument, `useBranches`' key is `branchKeys.list(providerId, repository, params ?? {})`
(`lib/hooks/api/useBranches.ts:7-8`), and the page passes `{}` — so "enabled only
while the popover is open", "keys match once open with an empty search", and
"eager unsearched request on load" are all literally true. The query itself is
unchanged in the diff (still `useBranches(providerId, repository, {}, …)`), as the
brief required. Comment-only, as instructed.

### 4. "Included PRs/MRs" casing — ADDRESSED

`lib/strings.ts:90` `couldNotLoadIncludedChanges: "Could not load Included PRs/MRs"`;
`SelectChangesPage.tsx:261` uses it in place of the `.toLowerCase()` template.

Test: `SelectChangesPage.test.tsx:258-269`.

**My mutation** — restored the `.toLowerCase()` template (run together with the
item-6 mutation, in a different file, so the two failures are distinguishable):

```
 FAIL  SelectChangesPage.test.tsx > SelectChangesPage > names the Included PRs/MRs section with its product casing when the changes fetch fails
TestingLibraryElementError: Unable to find an element with the text: Could not load Included PRs/MRs.
```

Restored → 407/407.

### 5. Duplicated STATUS_LETTER / STATUS_COLOR — ADDRESSED

`components/features/review/fileStatus.ts` is the single definition
(`:7-12`, `:14-19`); `FileHeader.tsx:4` and `FileTreeRow.tsx:2` import it and
both local copies are deleted (diff removes 13 lines from `FileHeader`, 18 from
`FileTreeRow`). The token conversion was done once, in the shared module, as the
brief asked. My item-2 mutation (single edit to the shared map, failure observed
in a `FileHeader`-rendered badge) is direct evidence there is no second copy
being read.

### 6. Shared discard-failure copy — ADDRESSED

`ReviewPage.tsx:126` `toast.error(messageFor(error, strings.reviewDiscardFailed))`,
identical to its sibling `ReviewsPage.tsx:49`.

Test: `ReviewPage.test.tsx:246-257`.

**My mutation** — restored the hardcoded literal:

```
 FAIL  ReviewPage.test.tsx > ReviewPage (READY) > reports a failed finish with the shared review-close copy
AssertionError: expected "vi.fn()" to be called with arguments: [ Array(1) ]
-   "The review could not be discarded.",
+   "The review could not be closed.",
```

Restored → 407/407. The implementer's reachability note is correct and I confirm
the landed test uses `HttpResponse.error()` (`ReviewPage.test.tsx:251`), which is
the only way `messageFor`'s fallback branch is entered — a JSON:API 500 with a
`title` would have made the assertion unreachable.

### 7. Cancel button and scrim tests — ADDRESSED

`ReviewsPage.test.tsx:119-141`. Two mutations, run separately so specificity is
demonstrated, not assumed.

**My mutation A** — `NewReviewSheet.tsx:166` `onClick={() => close(false)}` →
`onClick={() => undefined}`:

```
 FAIL  src/pages/__tests__/ReviewsPage.test.tsx > ReviewsPage > closes the drawer when Cancel is clicked
 Test Files  1 failed | 44 passed (45)
      Tests  1 failed | 406 passed (407)
```

Only the Cancel test failed — the scrim and Escape tests still passed.

**My mutation B (scrim)** — added `onInteractOutside={(event) => event.preventDefault()}`
to `SheetPrimitive.Content` in `components/ui/sheet.tsx:66`, which severs
outside-dismissal while leaving Escape and Cancel intact:

```
 FAIL  src/pages/__tests__/ReviewsPage.test.tsx > ReviewsPage > closes the drawer when the scrim is clicked
 Test Files  1 failed | 44 passed (45)
      Tests  1 failed | 406 passed (407)
```

Only the scrim test failed. **On the surviving-mutant disclosure:** the report's
account is technically correct and honest. Radix's `DismissableLayer` binds
`pointerdown` on the document, so `preventDefault` on the overlay element's own
handler cannot stop dismissal — that first attempt was genuinely a survivor, and
recording it rather than discarding it is the right behaviour. My independent
mutation B confirms the landed test does pin the behaviour. One accurate caveat:
because dismissal is document-level, the test pins "a click on the scrim element
closes the drawer" via the outside-dismissal path; it cannot pin that the overlay
element itself handles the event, because in Radix it does not. That is the
correct level to test at, and the test's `expect(scrim).not.toBeNull()` guard
(`ReviewsPage.test.tsx:135`) means a renamed `data-slot` fails loudly instead of
silently clicking nothing.

## New breakage introduced by the fix diff

**None at Critical or Important.** Lint, format, types, the full suite and the
build are all clean; every changed file is accounted for by a brief item; nothing
in the DO-NOT-FIX list was touched (verified: `FileTree.tsx` and the ARIA
attributes, the `useBranches` call itself, `providerLink.ts`, `ReviewRow`'s
`pending` prop, and `ReviewStatusLine` are all absent from the diff).

Minor, non-blocking, introduced by this diff:

- `ReviewRow.tsx:12-13` (`bg-success` / `bg-warning`) and `FileTreeRow`'s use of
  `STATUS_COLOR` are not asserted by any test — my item-2 mutation produced
  failures only from the `FileHeader` consumer. These class names would survive a
  mutation. This is a pre-existing coverage shape (the raw palette classes were
  equally unpinned) that the fix neither worsens nor closes, and FE-06 is a
  static-inspection check that passes by reading the source. Flagging as a
  coverage note only.
- `strings.ts:90` introduces a second string that embeds `"Included PRs/MRs"`
  independently of `strings.includedChanges` (`:9`); the two can now drift. The
  comment explains the choice and it is the simplest fix, but a reader may prefer
  a composed form. Non-blocking.

Deferred minor, NOT introduced by this diff: an untracked `node_modules/.vite/vitest`
cache directory exists at the worktree root (timestamped before this review
session, not in any commit, and not covered by `.gitignore`, which only ignores
`apps/frontend/node_modules/`). Housekeeping only.

## Tree cleanliness

Every mutation I applied was reverted with an inverse `Edit`, and the
`npm run build` I ran deleted `apps/backend/internal/ui/dist/.gitkeep`, which I
restored with `git checkout --`. Final state:

```
$ git status --short
?? node_modules/
$ git diff --stat -- apps/
(empty)
```

`git diff -- apps/` is empty and `git status --short` shows no tracked
modification. The only entry is the pre-existing untracked vitest cache noted
above. **No mutant leaked into the branch.** Nothing was committed or pushed.

## Overall verdict

**PASS.** All 7 findings ADDRESSED, each of 1/2/4/6/7 proven by a mutation I
performed myself against the full suite with quoted real failure output (6
mutations, 6 caught, 0 survivors). The three new design tokens are correctly
defined in both modes, correctly registered in `@theme inline`, carry values that
exactly match the `--destructive` 600/400 convention, and demonstrably emit CSS in
the shipped bundle. The `baseName` deviation is equivalent and broke no caller.
No new Critical or Important breakage.
