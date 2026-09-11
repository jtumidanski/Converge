# Execution notes — session 6 (Tasks 26, 27, 30, 28)

Session 6 ran Task 26 (the two cross-cutting security assertions), Task 27 (the hosted
integration test), a **new Task 30** added by user decision mid-session, and Task 28
(documentation, ops, and the full CI gate). Three fix rounds total.

| Task | Commits | Outcome |
| --- | --- | --- |
| 26 | `d8e5922..5701497` | spec ✅, 1 fix round, **1 parked** |
| 27 | `5701497..14b04b1` | spec ✅, Approved, 0 fix rounds |
| 30 | `14b04b1..23817e0` | spec ✅, Approved, 2 fix rounds |
| 28 | `23817e0..90a7490` | **review not yet dispatched** |

## The session's main finding: a real hosted-mode redaction gap

Grounding a ruling on Task 26 surfaced a product defect the plan never anticipated.

`internal/app/app.go:241` builds **one** shared `gitx` runner with `Secrets: cfg.Secrets()`,
fixed at construction, and `app.go:199` builds the log redactor from the same list.
`internal/config/config.go:104-118` shows `Secrets()` collects only **env-configured**
provider tokens plus the master key. In hosted mode provider tokens live **per-user in the
database**, so no hosted token was in either list.

The user was asked and chose to **fix it inside task-005** rather than defer it. That became
Task 30: per-invocation secrets on `gitx.Spec`, declared in each provider's `AuthorizeGit`,
redacted as the union of the runner's static list and the spec's own.

**An important correction to the narrative.** The first report framed this as a token "logged
in clear", and that overstates present reachability. The Opus review established there is **no
currently reachable production path** by which a raw token lands on git stderr — in git's world
the token exists only as base64 inside an `Authorization: Basic` blob. The failing test
manufactured the condition via test-local `Spec.Args`. The honest statement is *"would be
logged in clear if it ever surfaced"*: this is defence in depth, not closure of a live leak.

That correction is what drove fix round 1 — since the **blob** is the form with production
reality, declaring only the raw token was the less useful half of the fix.

## Rulings

**Task 26 — the `"token"` assertion narrowed to `"token":`.** The brief's literal
`strings.Contains(body, "\"token\"")` would fire on the brief's own vacuity guard
`"tokenLast4":"9f2c"`, making two assertions in one loop mutually unsatisfiable.
*Cost if wrong:* a bare `"token"` key with no colon slips through. No such JSON shape exists.

**Task 26 — unreachable routes still issued, error bodies captured.** Dropping routes that
cannot reach 2xx would quietly shrink the specified coverage.
*Cost if wrong:* none material.

**Task 26 — Finding 1 PARKED (credential-injection path unexercised).** The prescribed
option-(b) fix (drive review create via the `fake` kind) **relocated rather than closed** the
problem: `fake.Provider.AuthorizeGit` (`internal/provider/fake/fake.go:177`) is a hardcoded
no-op, so `GIT_CONFIG_*` is still never populated. Parked on three grounds — the brief itself
mandates a fake provider, so closing it means contradicting the brief; Task 27's brief
specifies a fake provider too, so it cannot close it either; and `gitx` **never logs `Args` or
`Env`** (`exec.go:159-182` logs only category, repo, session, exit code, duration, and
*redacted* stderr), so the worry is answered by the code even though no test proves it.
*Cost if wrong:* if future code ever logs `Spec.Args`/`Spec.Env`, no test in this plan catches
it. **Routed to the final review as a named residual.**

**Task 30 — the bare-blob Minor taken into a fix round** rather than deferred. Minors do not
normally enter the loop, but the review established the blob is the form that actually exists
in production while `AuthorizeGit` declared only the raw token; `headerRe` covers the blob only
when its `authorization:` prefix is present.
*Cost if wrong:* a few more bytes in a redaction list.

**Task 30 — the untested GitLab declaration taken into a second fix round**, overriding the
reviewer's "deferred minor". `BasicAuthBlob` had zero references in any GitLab test, and the
append at `gitlab/client.go:243` could have been deleted with the suite still green. Task 30
exists *because* a redaction list silently failed to cover an unasserted case; reproducing that
shape inside its own fix would be self-defeating.
*Cost if wrong:* ten lines of duplicated test.

## What the reviews established

**Task 27's in-test resolver does not hollow out the isolation claim.** `provider.Resolver`
(`internal/provider/resolver.go:20`) is a public interface whose doc comment says the hosted
implementation "satisfies this interface from the outside", and `auth.ProviderResolver`
declares `var _ provider.Resolver = (*ProviderResolver)(nil)` (`resolver.go:60`). The test's
stub occupies the identical position; `mirror.Cache`, `session.Store`, `review.Service` and
`gitx.ExecRunner` are all production types. Also: `package review` was correct and the
controller's dispatch constraint repeating the brief's `review_test` was **wrong** — every test
file in that package is `package review`.

**Task 30's blast radius is clean.** `secretsFor` returns `opts.Secrets` *by identity* when a
spec declares none (`exec.go:113-115`), so standalone is bit-for-bit unchanged; the non-empty
branch allocates at exact capacity so it cannot append into shared backing storage. All 36
`gitx.Spec{` literals in the tree are keyed. `BasicAuthBlob` is a genuine single derivation —
`CredentialEnv` calls it and the old inline base64 was removed, not duplicated.

**The `redactingHandler` second surface is unreachable**, independently re-verified: a
`Reveal()` grep returns exactly ten non-test hits, none flowing to a log attribute;
`config.Secret` redacts via `LogValue`/`String`/`MarshalJSON`, and every decrypted hosted token
is wrapped at `internal/auth/crypt.go:87`.

## The flake — now close to ruled out

Sighted twice in sessions ≤4 by two different agents under full-suite load. **Not reproduced
once in sessions 5 or 6**, across roughly twenty consecutive runs including a dedicated
`go test -race -count=3 ./internal/auth/` hunt (Task 27) and a **full `make test` +
`make test-integration` CI gate from the repo root** (Task 28). Every agent was instructed
never to retry-until-green and to report failures verbatim; none had a failure to report.
This is strong evidence. The final review should make the call explicitly rather than let it
lapse.

## Carry forward

1. **Task 28's review was never dispatched** — the context-handoff guard refused it at 62
   controller tool calls. It is the **first action of session 7**. Its adjudication items are
   recorded in the ledger; the review package is already generated at
   `.superpowers/sdd/plan/review-23817e0..90a7490.diff`. Because Task 28 is a *documentation*
   task, the review must invert its usual method: a doc's defect is being **wrong**, not being
   ugly. Highest-value checks: every env-var name/default against `internal/config/config.go`
   (a confidently-stated wrong default is Critical); the `X-Forwarded-For` throttle-forging
   claim against `internal/api/authctx.go`'s `clientIP`; the five plan-text corrections being
   *correct* and not merely changed; correction (5) genuinely being unlocatable; and
   spot-checking that the 35 claimed criterion-to-test mappings name tests that **actually
   exist** — a table asserting false coverage is worse than no table.
2. **Then Task 29**, the final whole-branch review, on the most capable model, pointed at the
   ledger's parked and deferred-minor lines.
3. **Deferred minors for the final review to triage:** Task 26's parked Finding 1 (above);
   `ExitError.Result.Stderr` is a raw exported field that could carry unredacted stderr via
   `slog.Any` under the JSON handler (pre-existing, no caller does it today, a `LogValue()`
   follow-up); `spec_secrets_test.go`'s GitLab-filename asymmetry; Task 24's native `<select>`
   instead of the shipped Radix `Select` (the only one with design weight); Task 25's
   `renderPageWithClient` duplicating `render.tsx`; Task 22's non-exhaustive mutation coverage.
4. **`npm run build` dirties `apps/backend/internal/ui/dist/.gitkeep`.** Bit three tasks. Task
   28 handled it cleanly; keep checking `git status` before committing.

## Resuming

`/clear`, then `/execute-task task-005` from this worktree. Base is `90a7490`. All briefs
(26–29) are already extracted in the SDD workspace; no task-brief extraction is needed.
