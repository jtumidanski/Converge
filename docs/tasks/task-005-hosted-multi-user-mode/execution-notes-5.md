# Execution notes — session 5 (Tasks 22–25)

Session 5 ran the four frontend screens: Task 22 (mode gate, auth provider, route guard,
account menu), Task 23 (login and register), Task 24 (provider settings), Task 25 (account
settings). One fix round was needed, on Task 23. All four tasks passed their reviews.

| Task | Commits | Outcome |
| --- | --- | --- |
| 22 | `a872dea..1b4b7fe` | spec ✅, Approved, 0 fix rounds |
| 23 | `1b4b7fe..13bea39` | spec ✅, Approved after 1 fix round |
| 24 | `13bea39..b03c50c` | spec ✅, Approved, 0 fix rounds |
| 25 | `b03c50c..1b780c0` | spec ✅, Approved, 0 fix rounds |

## Rulings

**Task 22 — placeholder pages accepted, with a carried obligation.** The brief's own
`routes.tsx` sample imports four pages that Tasks 23–25 build, and its tests 11–12 are
unsatisfiable without them, so the task as written could not compile in isolation. The
implementer created minimal placeholders — bare `PageHeader`s with doc comments naming the
replacing task. Accepted, with the mitigation that Tasks 23/24/25 were each told to *replace*
their placeholder in place rather than add a parallel component. **All three reviews confirmed
in-place replacement, so this obligation is now fully discharged.**
*Cost if wrong:* a placeholder ships mistaken for a finished screen.

**Task 22 — `App.test.tsx` call-site churn accepted.** A module-level singleton `QueryClient`
with `staleTime: Infinity` leaked cache across tests once `ModeGate` existed; the fix touched
every `render(<App />)` call site. Global Constraint 18 forbids adding assertions to
pre-existing tests, not changing how they construct their subject, and the leak was a real
isolation defect `ModeGate` merely exposed. The reviewer verified byte-for-byte that every
`expect()` is character-for-character unchanged.
*Cost if wrong:* a pre-existing test quietly stops asserting, and a green suite no longer
proves standalone equivalence.

**Task 23 — pre-existing assertion change accepted.** Two `routes.test.tsx` queries went from
`getByText("Log in")` to `getByRole("heading", {name:"Log in"})`. `getByRole("heading")` is
strictly narrower than `getByText`, so the suite proves more, not less; and the ambiguity was
forced — a page with both a "Log in" heading and a "Log in" submit button makes `getByText`
throw, so no correct implementation could have left the old query standing. The reviewer
confirmed exactly two lines changed and nothing else in the file.
*Cost if wrong:* if other assertions had been touched, standalone equivalence would no longer
be proven by that suite.

**Task 23 — open-redirect fix ordered rather than deferred.** The review found that `safeNext`'s
guard rejected `//evil.test` but not `/\evil.test`, which the WHATWG URL parser resolves to the
same attacker origin because browsers treat `\` as `/` for special schemes. The brief mandated
the vulnerable guard verbatim, so this was a finding against plan text. Fixed anyway: a helper
whose own doc comment claims open-redirect safety while not providing it is a defect regardless
of the sample, and **Task 25 was slated to reuse it**, so deferring would have let a second
screen build on the hole. The prescribed shape was additive — reject a backslash anywhere,
keep the three existing checks — and the `new URL(...).origin` alternative was explicitly
forbidden for dragging `window.location` into a pure helper.
*Cost if wrong:* a legitimate path containing a backslash falls back to `/`. No such path exists.

## What the reviews established

**The narrowed 401 contract holds, structurally.** This risk has been tracked since Task 21.
On the account settings screen a mistyped current password cannot log the user out *by
construction*: `ChangePasswordForm`'s catch calls only `applyServerError` and the component
never imports `useQueryClient`; `useChangePassword` invalidates only on success; and
`DeleteAccountForm`'s `queryClient.clear()` sits after the `await` inside the `try`, so a
thrown `INVALID_CREDENTIALS` jumps to `catch` and cannot reach it. Zero blanket
`status === 401` handling and zero `localStorage`/`sessionStorage` use exist anywhere in the
four screens.

**`validate: true` is correct, not filler.** Task 24 hardcoded it because the Zod schema has
no such field. Verification against `settings_providers.go:96-117` showed `Validate *bool` is
real, defaults to `true` when absent, and drives server-side credential verification — so the
hardcoded value matches the server's own default.

**Provider tokens never leave the server.** Only `•••• {tokenLast4}` is rendered, a test
asserts no `ghp_` pattern appears, and the blank-token-keeps-stored-token rule is enforced at
two independent layers (the form omits the key; the service drops a falsy token).

## Measurement notes

Two implementers caught their own **non-discriminating mutations** this session. Task 24's
first mutation could not have failed, because the service layer already strips a falsy token
independently. Task 25's was masked by a `gcTime` setting on the local test `QueryClient`;
after correction, removing `queryClient.clear()` is caught. Both were re-run properly and both
were routed to the reviewer for an independent check. This is the measurement discipline
working as intended, and it is worth keeping the habit of reporting a failed mutation attempt
rather than quietly replacing it.

## Carry forward

1. **The flake.** The intermittent `internal/auth` timing flake, sighted twice in earlier
   sessions by different agents under full-suite load, did **not** appear in any of session 5's
   runs. That is evidence, not proof — a frontend-heavy workload says little about it. It must
   still be reproduced and fixed, or **positively ruled out**, before the PR. It belongs to
   Task 27 and the final review.
2. **Deferred minors for the final review to triage.** Task 24's native `<select>` in place of
   the shipped Radix `Select` is the only one with design weight: `components/ui/select.tsx`
   exists and `ProviderPicker.tsx` already uses it, so "untested in jsdom" was a test-authoring
   gap, not a constraint. Decide it deliberately rather than letting it become a standing
   pattern. The rest are cosmetic: Task 25's `renderPageWithClient` duplicating `render.tsx`'s
   provider stack; Task 22's `ErrorBanner` prop-name slip (in its report only, not the code);
   Task 22's non-exhaustive `ModeGate`/`AccountMenu` mutation coverage; Task 21's mildly
   redundant `client.test.ts` 401 tests.
3. **Task 28 still owes four documentation corrections** carried from sessions 3 and 4 — they
   are listed in `execution-notes-4.md`.
4. **`npm run build` dirties the tree** — it writes into `apps/backend/internal/ui/dist` and can
   leave `dist/.gitkeep` modified. Restore it before committing.

## Resuming

`/clear`, then `/execute-task task-005` from this worktree. Next task is **Task 26** (the two
cross-cutting security assertions), base `1b780c0`. Briefs through Task 25 are already in the
SDD workspace; Task 26's must be extracted with `scripts/task-brief`.
