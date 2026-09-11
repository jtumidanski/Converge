# Audit — task-005 hosted multi-user mode

This is Task 29 Step 4: the whole-branch code review record. Four reviewers audited the branch
independently, one fix wave addressed their findings, and the four reports are reproduced verbatim
below under their own headings.

## Verdicts

| Reviewer | Verdict | Findings |
| --- | --- | --- |
| Plan adherence | Adherent | 29/29 plan tasks implemented |
| Backend DOM/SUB/SEC checklist | Approve with findings | No Critical |
| Frontend FE-\* checklist | Approve with findings | No Critical |
| Whole-branch merge review | Approve with findings | No Critical |

Across all four: **no Critical findings, no cross-tenant data path, no standalone-mode regression,
and all five CI gates green.** Standalone equivalence — the branch's requirement #1 — holds: with
no new environment variables set the server starts in standalone mode, creates no database file,
and the pre-existing suites pass unchanged.

## What the fix wave changed

Ten items were dispatched. Nine were implemented; one was rebutted with evidence (below).

| # | Item | Outcome |
| --- | --- | --- |
| 1 | Throttle sweeper reaped user-scope failure counters, so paced guesses never reached the 5-failure lockout | Fixed: `DeleteElapsedAttempts` now carries a `scope = 'ip'` predicate |
| 2 | A username lockout outlived its account and was inherited by the next registrant | Fixed: `DeleteUser` deletes the user-scope counter in the same transaction |
| 3 | `docs/hosted-mode.md` told operators not to set `CONVERGE_MODE=hosted` for `converge-cli`, which has been unnecessary since `fc657d4` | Fixed: the passage now describes the guarantee the code provides |
| 4 | `providerResolverTTL` documented a bound on decrypted-token residency that nothing enforced | Fixed: `ProviderResolver.EvictElapsed`, called from `Service.Sweep` |
| 5 | `store.go` claimed to be the only file in the repository containing SQL | Fixed: the claim is now scoped to queries against application tables |
| 6 | `UserProviderForm` used a native `<select>` where the rest of the UI uses the Radix `Select` | Fixed: swapped, matching `ProviderPicker` |
| 7 | ~75 of 86 lines duplicated verbatim between the create and edit form bodies | Fixed: extracted `UserProviderFormBody` |
| 8 | A wall-clock median-of-three 2x ratio assertion under `t.Parallel()` with `-race` | Fixed: replaced with an allocation measurement and taken out of parallel execution |
| 9 | A test comment still framed a defence-in-depth guard as a reproduction of a real leak | Fixed: comment rewritten |
| 10a | `settings_providers.go` echoing `err.Error()` into a 422 body | **Rebutted** — see below |
| 10b | A store fault during login reported as 401 `INVALID_CREDENTIALS` | Fixed: a fault now propagates unclassified (500) and is not counted against the throttle |

Items 1, 2, 4 and 10b were each demonstrated with a failing test before being fixed. Four
pre-existing tests in `internal/auth` asserted the defective sweep and cascade semantics
(`TestSweepRemovesElapsedRows`, `TestDeleteUserCascades`, `TestDeleteElapsedAttempts`,
`TestSweepRemovesExpiredSessionsAndElapsedLockouts`); they were updated to assert the corrected
behaviour, and each now also pins the narrowness of the fix — that the sweeper still reaps stale
IP counters, and that deleting one account does not clear another's counter.

## Rebuttal: item 10a is not a defect

**Claim:** `apps/backend/internal/api/settings_providers.go:184` echoes raw `err.Error()` into a
422 response body, which can surface provider-API internals to a client.

**Why it does not hold.** That line is reached only for an error satisfying
`errors.Is(err, auth.ErrInvalidInput)`. Every site that wraps that sentinel is in `internal/auth`,
and there are exactly six — `providers.go:52,191` and `model.go:64,86,92` — each a static,
value-free sentence authored in this repository:

- `auth: token is required`
- `auth: kind must be github or gitlab`
- `auth: a slug is 1 to 32 characters of lowercase letters, digits, and hyphens, starting with a letter or digit`
- `auth: base url is required for gitlab providers`
- `auth: base url must be an absolute http(s) URL`

None interpolates a client value, and none wraps an error from a provider API, the database, or
the crypto layer. A provider-API failure during token validation is returned as
`*auth.Error{Code: CodeProviderUnauthorized}`, which is classified by `writeDomainError`, not by
this branch. `writeProviderSettingsError`'s own doc comment states the design: it fails closed, and
`ErrInvalidInput` is the allow-list of messages deliberately kept safe to echo. Changing this to a
fixed message would lose the only thing that tells a user *which* field they got wrong, for no
confidentiality gain.

**Residual risk accepted:** the echoed detail includes the wrapped sentinel text
(`": auth: invalid input"`). That is cosmetic, discloses nothing, and is left alone.

## Deferred, deliberately

These were ruled out of this wave and are not defects on the merge path:

- **DOM-01 / DOM-IMM** — no `builder.go` in `internal/auth`, and `UpdateProvider` assigning fields
  directly. `row` is a local copy of an immutable value type, so nothing shared is mutated, and the
  plan's file structure deliberately omits `builder.go`.
- **FE-03** (`AuthProvider.tsx` importing `setUnauthorizedHandler` from the client) and **FE-10**
  (`types/models/auth.ts` using flat interfaces rather than `Resource<>`). FE-10 is a real
  consistency wart, but reshaping it touches the API typing surface — not a final-wave change.
- `renderPageWithClient` duplicating `test/render.tsx` — fast-follow.
- The plan's unticked checkboxes, and `f916444`'s message describing a gitignored log as
  committed. Record-keeping; this file and the reports below are the record.

## Residual gap worth naming

Two smaller notes from the fix wave itself:

- The Radix `Select` swap is covered by tests that assert the control renders the correct value in
  both form variants, and by the existing create/edit payload tests (which pin `kind`). There is no
  test that *changes* the kind through the popup: Radix's pointer-event protocol needs jsdom
  shims this repository does not yet have, and no in-tree test drives a `Select` today.
- Narrowing the throttle sweeper means a user-scope counter now has exactly two exits: a successful
  login, and account deletion. A counter for a username that is guessed at but never logged into
  persists. That is bounded by distinct usernames attempted, and the per-IP lockout bounds the rate
  at which an attacker can create them; it is the correct trade against FR-7.2, but it is a growth
  vector worth remembering if the attempt table is ever observed to be large.

---

# Reviewer report — plan adherence

# Plan Adherence Audit — task-005-hosted-multi-user-mode

**Plan:** `docs/tasks/task-005-hosted-multi-user-mode/plan.md` (7124 lines, 29 numbered tasks + Task 30)
**Audit date:** 2026-09-11
**Branch:** `task-005-hosted-multi-user-mode` @ `f916444`
**Range audited:** `7ae00b9..f916444` (50 commits, 140 files, +24.7k/−271)
**Scope:** Tasks 1–28 and Task 30. Task 29 (this review) is excluded.
**Mode:** read-only. No working-tree, index, HEAD, or branch mutation. `make build` wrote
gitignored artifacts under `dist/` and `apps/backend/internal/ui/dist/` only; `git status`
after the gates shows no tracked change.

---

## Method

Evidence was gathered mechanically rather than by reading the plan prose end to end:

1. **Test existence.** Every `Test*` identifier named anywhere in `plan.md` (177 distinct
   names, including the 44 declared as full `func Test…` bodies in the plan's code blocks)
   was checked against `grep -rE "func <name>\b" apps/backend`. **All 177 exist.** Zero
   plan-specified Go tests are missing.
2. **Frontend test existence.** The frontend tasks (21–25) specify tests as numbered prose
   assertions rather than code. Each of the 59 numbered items was matched against the actual
   `it("…")` titles in the created test files. All are present (see per-task rows).
3. **Exported surface.** Every symbol in each task's `- Produces:` block was checked by
   signature-literal grep against the implementation files (not tests).
4. **Dependency direction.** Verified with `go list -deps`, not by reading imports.
5. **Gates.** Run in the foreground, one at a time, from the worktree root. `make docker-build`
   read from `.superpowers/sdd/plan/task-28-gate.log` per brief.

The deliberate-deviation list in `.superpowers/sdd/plan/task-29-brief.md` (7 items) and the
adjudicated items in `.superpowers/sdd/plan/residuals.md` (8 items) were read first and are
**not** reported as findings. Each was independently spot-checked in the code; see
"Adjudicated items re-verified" below.

---

## Per-task adherence

| # | Task | Verdict | Evidence |
|---|---|---|---|
| 1 | `internal/identity` — Scope value | **ADHERENT** | `internal/identity/scope.go:20,23,26,29,37` — `Standalone`, `ForUser`, `UserID`, `IsScoped`, `Matches` with unexported `userID`, exact plan names. `scope_test.go:51` `TestZeroValueIsStandalone`; `TestMatchesTruthTable` present. `go list -deps ./internal/identity` → only itself (stdlib leaf). |
| 2 | `config` — mode selection + hosted vars | **ADHERENT** | `internal/config/config.go:32,35` (`ModeStandalone`/`ModeHosted` with the exact string values), `:82-91` (`DatabasePath`, `SecretKey`, `SecureCookies`, `TrustedProxy`, `LoginSessionTTL`, `LoginIdleTTL`, `IgnoredProviderVars`), `:225-229` mode parse defaulting to standalone, `:274,277` TTL defaults 720h / 168h as specified. `config_test.go:139` `TestModeDefaultsToStandalone`, `:165` `TestStandaloneStillRequiresAProvider`. New test functions appended; **no pre-existing `config_test.go` function was edited** (`git diff` shows +143/−0). Minor signature drift: `loadProviders` returns 3 values (`config.go:283`) not the 2 the plan wrote, because it also returns `IgnoredProviderVars` — which the plan itself specifies as a `Config` field. Unexported, no caller contract. |
| 3 | `internal/db` — SQLite + migration runner | **ADHERENT** | `internal/db/db.go:38` `Options`, `:52` `Open`, `:83` `Migrate`, `:35` `ErrSchemaAhead`, `:101` schema-ahead guard. `migrations/0001_init.sql` present (57 lines). `db_test.go` +134. `go list -deps ./internal/db` → only itself (stdlib + driver leaf) ✓. `schema_migrations` created by the runner (`db.go:85`) with the omission documented at `0001_init.sql:6` — deviation #1, approved. Binary size cost captured in `task-28-gate.log`. |
| 4 | `auth` errors and models | **ADHERENT** | `internal/auth/errors.go:23-32` — all ten `Code*` constants with the exact wire strings (`UNAUTHENTICATED`, `INVALID_CREDENTIALS`, `ACCOUNT_LOCKED`, `USERNAME_TAKEN`, `INVALID_USERNAME`, `WEAK_PASSWORD`, `FORBIDDEN`, `PROVIDER_SLUG_TAKEN`, `PROVIDER_UNAUTHORIZED`, `PROVIDER_IN_USE`). `model.go`: `NewUser`, `Fold`, `ValidateUsername`, `ValidateSlug`, `NormalizeBaseURL`, `NewID`, `Last4`, `User`/`LoginSession`/`UserProvider` accessors all present. `model_test.go` +126. `NewID` is 16 hex chars (`model.go:24,31` `idBytes = 8`) — deviation #2, approved and documented in-code. |
| 5 | `auth/password.go` — Argon2id | **ADHERENT** | `password.go:28-33` — `argonTime=1`, `argonMemory=64*1024` (64 MiB), `argonKeyLen=32`, `argonSaltLen=16`, `argonVersion=19`. `HashPassword`, `VerifyPassword`, `ValidatePassword`, `ErrPasswordMismatch`, `MinPasswordLen`/`MaxPasswordLen`, `dummyHash` all present. **`TestVerifyReadsParamsFromTheString` survives intact and still calls `argon2.IDKey` directly** — `password_test.go:85` builds a weak-parameter PHC string by hand (`m=8,t=1,p=1`) and verifies against it; it was not simplified into a `HashPassword` round-trip. Bounds guards added in the `01cf6e2` fix round (`password.go:43-44` `maxArgonMemory`, `maxArgonTime`) are additive hardening, not a weakening. |
| 6 | `auth/crypt.go` — AES-256-GCM | **ADHERENT** | `crypt.go:4,38,44` AES-256-GCM via `crypto/aes` + GCM; `:53` `aad(userID, providerID)` binds the ciphertext to its exact row per FR-5.3; `:67,78` nonce length enforced from `aead.NonceSize()`. `NewSealer`/`Seal`/`Open` signatures match the plan exactly. `crypt_test.go` +137. |
| 7 | `auth/store.go` — all the SQL | **ADHERENT** | All 23 plan-specified `*Store` methods present in `store.go` (checked by `func (s *Store) <name>(` literal). **SQL confinement holds:** the only non-test Go files in the repo containing `SELECT`/`INSERT INTO`/`UPDATE`/`DELETE FROM`/`CREATE TABLE` are `internal/auth/store.go` and `internal/db/db.go` (the migration runner, which the plan designates). `store_test.go` +585. |
| 8 | `auth/throttle.go` — doubling lockout | **ADHERENT** | `throttle.go` — `NewThrottle`, `Check`, `Fail`, `Succeed`, `Sweep` with the plan's signatures. `throttle_test.go` +344. Counters persisted in the database (commit `f8dc70b`) and the `Fail` counter made atomic in the `32f668e` fix round. |
| 9 | `provider.Resolver` + mechanical rewire | **ADHERENT** | `internal/provider/resolver.go:20` `type Resolver interface`, `:27` `NewStaticResolver`. `review.Deps.Providers` and `api.Deps.Providers` are both `provider.Resolver`; `app.App:55` gains `Resolver provider.Resolver` beside the existing `Registry`. Call-site churn is purely mechanical — every diff hunk in `api_test.go`, `harness_test.go`, `main_test.go` is a `registry` → `provider.NewStaticResolver(registry)` substitution with **no assertion added or removed**. |
| 10 | `session` — owner field + scoped store | **ADHERENT** | `Session.Owner()`, `Builder.SetOwner`, `Record.Owner` with `json:"owner,omitempty"`, and the five scoped store methods (`Get(id, scope)`, `List(scope)`, `Finish(ctx, id, scope)`, `Unowned()`, `Purge(ctx, userID)`) all present with the plan's exact signatures. `record_test.go:285` `TestOwnerRoundTripsThroughTheRecord`, `:309` `TestRecordWithoutOwnerDecodesToEmpty` (the no-migration guarantee), `store_test.go:767,809,835` the scoping contracts. Placement-only deviation: the owner tests landed in `record_test.go`/`store_test.go` rather than `model_test.go`; all plan-named tests exist. `Save`/`SaveActive`/`Corrupted`/`LoadAll`/`RunSweeper`/`Root`/`Dir` unchanged as required. |
| 11 | `mirror` — per-user namespaces | **ADHERENT** | `cache.go` — `Namespace`, `RootNamespace()`, `NamespaceFor(scope)`, and the four namespaced methods `Path`/`Ensure`/`FetchSHA`/`PurgeNamespace` with `ns Namespace` in the plan's position. `NamespaceRoot` at `:97-101` refuses the root namespace and `PurgeNamespace:108` routes through it — deviation #7, approved, with the rationale in the doc comment at `:104-107`. `cache_test.go` +205 (new functions; existing bodies only mechanically rewired, `objects_test.go` diff is two `RootNamespace()` insertions). |
| 12 | `review` — thread the scope | **ADHERENT** | All nine plan-specified `*Service` signatures present verbatim, including `PurgeUser` (implements `auth.Purger`) and `ProviderInUse` (implements `auth.ProviderUsage`). `(*Resolver) Resolve` gains `ns mirror.Namespace` after `ctx` (`resolve.go`; confirmed by the `resolve_test.go` call-site diff). `service_test.go:1072` `TestCreateInStandaloneLeavesOwnerEmpty`. `StartBuild`/`Build`/`Corrupted` unchanged. |
| 13 | `auth` — verify + per-user resolver | **ADHERENT** | `verify.go` — `ProviderVerifier`, `NewHTTPVerifier`. `resolver.go` — `ProviderResolver`, `NewProviderResolver`, `Resolve(ctx, scope) (*provider.Registry, error)`, `Invalidate(userID)`, the `var _ provider.Resolver` assertion, and `providerResolverTTL = 15 * time.Minute` at the plan's exact value. `resolver_test.go` +312. |
| 14 | `auth/service.go` — account lifecycle | **ADHERENT** | `service.go` — `Purger`, `ProviderUsage`, `ServiceDeps` (all 11 fields), `NewService`, `Credentials`, and all ten methods (`Register`, `Login`, `Logout`, `Authenticate`, `User`, `ProviderCount`, `ChangePassword`, `DeleteAccount`, `CountUsers`, `Ping`). `service_test.go` +606. The `ProviderUsage` collaborator beside the design's `Purger` is deviation #6, approved. |
| 15 | `auth` — provider CRUD + sweeper | **ADHERENT** | `providers.go` — `ProviderInput`, `ProviderPatch` (with `Token *string`, nil-or-empty ⇒ keep, per FR-5.5), and `CreateProvider`/`ListProviders`/`Provider`/`UpdateProvider`/`DeleteProvider`. `sweep.go` — `Sweep`. `providers_test.go` +635. |
| 16 | `api` — scope ctx, cookies, middlewares | **ADHERENT** | `authctx.go`/`authmw.go` — `scopeFrom`, `tokenHashFrom`, `sessionTokenFrom`, `setSessionCookie`, `clearSessionCookie`, `clientIP`, `originGuard`, `authenticate`, and `sessionCookieName = "converge_session"` (exact plan value). `router.go:50-62` — all seven new `Deps` fields at the plan's names and types. `authmw_test.go:89` `TestScopeFromDefaultsToStandalone`, +331 total. |
| 17 | `api` — `/api/auth/*` handlers | **ADHERENT** | `api/auth.go` +183, `auth_test.go` +401. `errors.go` +7 (the two `classify` arms), `errors_test.go` +22. **`TestAuthRoutesAre404InStandalone` is un-skipped and asserts substantively** (`auth_test.go:348-370`): it walks all six state-changing auth routes, requires 404, asserts the `NOT_FOUND` error code on each, then asserts `/api/auth/mode` still answers 200 in standalone. No `t.Skip` remains anywhere in the new auth/settings tests — the only `t.Skip` calls in the tree are the pre-existing `git not installed` / `root ignores directory permissions` environment guards. |
| 18 | `api` — `/api/settings/providers` | **ADHERENT** | `settings_providers.go` +188, `settings_providers_test.go` +575. **`TestSettingsRoutesAre404InStandalone` is un-skipped and asserts** (`settings_providers_test.go:537-552`): all four routes, 404 plus `NOT_FOUND` code. `ProviderPatch.Validate` defaults to `false` on PATCH (`settings_providers.go:141`) while POST defaults to `true` via a `*bool` (`:36-38,106-109`) — deviation #5, approved, with the reasoning in the doc comment at `:127-130`. The `7637842` fix round added fail-closed error classification (`:166-186`), which is stricter than the plan, not weaker. |
| 19 | `api` — scoped handlers, routes by mode, healthz | **ADHERENT** | `router.go:130-146` registers the ten hosted-only routes inside `if d.Mode == config.ModeHosted`, with the 404 coming from the pre-existing `/api/` catch-all — "the absence of a route *is* the behaviour", so no handler carries a standalone branch, as the plan requires. `:163-172` composes `authenticate` then `originGuard` only in hosted mode, so standalone's request path gains zero comparisons. `health.go:25-30` adds the `database` check key **only** when `DBPing != nil`, keeping the standalone `/healthz` body byte-identical. `isolation_test.go` +407 including `:377` `TestStandaloneRouterIsUnchanged`. Deviations #3 (`deleteReview` visibility in the handler) and #4 (`sessionFor` consults `Corrupted` only when unscoped) are both approved. |
| 20 | `app` and `cmd` — the wiring | **ADHERENT** | `app.go:55,64-66` — `Resolver provider.Resolver`, `DB *sql.DB`, `Auth *auth.Service`, `ProviderResolver *auth.ProviderResolver`; `:88` `Close` closes the database. `:326` builds the per-user resolver. `app_test.go` +247 including `:378` **`TestStandaloneCreatesNoDatabaseFile`** — the requirement-#1 guard. Step 6's by-hand two-mode smoke test is unverifiable from the artifact, but is covered mechanically by `TestStandaloneCreatesNoDatabaseFile` plus `cmd/converge`'s own suite (41s, passing). |
| 21 | frontend — client, types, schemas, services, hooks | **ADHERENT** | `client.ts` — `setUnauthorizedHandler`, `apiPatch`, `apiPostNoContent`, `apiDeleteWithBody` all exported. `types/models/auth.ts` — all five declared types. `services/api/auth.ts` — all seven methods. `services/api/userProviders.ts` — `UserProviderCreate`, `UserProviderPatch`, the four service methods. `useAuth.ts` — `authKeys` + all seven hooks at the exact plan names; `useUserProviders.ts` — `userProviderKeys` + all four. All 15 numbered test assertions present (`client.test.ts` 5/5 + 3 extra from the `a872dea` fix round discriminating `UNAUTHENTICATED` from `INVALID_CREDENTIALS`; `auth.test.ts` items 6–11 all present; `useAuth.test.tsx` items 12–14; `useUserProviders.test.tsx` item 15 ×3). |
| 22 | frontend — mode gate, auth provider, guard, menu | **ADHERENT** | `ModeGate.tsx`, `AuthProvider.tsx`, `RequireAuth.tsx`, `AccountMenu.tsx` all created; `AppShell.tsx`, `App.tsx`, `routes.tsx` modified. All 12 numbered assertions present: `ModeGate.test.tsx:20,40,50,64` (items 1–4), `RequireAuth.test.tsx:36,48` (5–6), `AuthProvider.test.tsx:39,64` (7–8), `AccountMenu.test.tsx:43,53` (9–10), `routes.test.tsx:40,50` (11–12), plus two extra guard tests at `routes.test.tsx:60,74`. |
| 23 | frontend — login and register pages | **ADHERENT** | `LoginPage.tsx`, `RegisterPage.tsx`. All 12 numbered assertions present: `LoginPage.test.tsx:31,57,74,93,111,126,142,151` (items 1–8) and `RegisterPage.test.tsx:30,58,64,78,92` (items 9–12). The open-redirect item (3) was hardened further in `13bea39` (backslash rejection in `safeNext`). |
| 24 | frontend — provider settings page | **ADHERENT** | `ProviderSettingsPage.tsx`, `UserProviderForm.tsx`, `UserProviderList.tsx`. All 12 numbered assertions present across `ProviderSettingsPage.test.tsx:31,44,61,70,90,112` and `UserProviderForm.test.tsx:29,75,97,119,143,156,170,195` — including item 3's two-part contract (token input empty on load with a keep-current placeholder, and a PATCH body with **no `token` key**) at `:156` and `:170`. Residual #4 (native `<select>` instead of the Radix `Select`) is a recorded frontend-reviewer question, not a plan gap. |
| 25 | frontend — account settings page | **ADHERENT** | `AccountSettingsPage.tsx`. All 8 numbered assertions present at `AccountSettingsPage.test.tsx:82,92,103,132,146,172,194,217,236` (item 3 split into two tests, item 6 its own). The typed-confirmation disable contract (item 4, FR-8.6) is at `:194`. |
| 26 | the two cross-cutting security assertions | **ADHERENT** | `internal/app/secrets_test.go:79` `TestNoSecretReachesTheLog` (+369), `internal/api/no_token_test.go:19` `TestNoResponseBodyEverContainsAToken` (+161). The `5701497` fix round made the log test actually exercise review creation. Residual #1 (the credential-injection path still unexercised because `fake.AuthorizeGit` is a no-op) is a recorded, adjudicated park — see my judgement below. |
| 27 | the hosted-mode integration test | **ADHERENT** | `internal/review/hosted_integration_test.go:1` `//go:build integration`, `:141` `TestTwoUsersReviewTheSameRepositoryInSeparateMirrors`, `:221` `TestHostedPruneDoesNotDeleteALiveReviewBranch` (+257). `make test-integration` green. |
| 28 | documentation, ops, full build gate | **ADHERENT (one stale paragraph)** | `README.md` +53, `.env.example` +15, `docker-compose.yml` +33, `docs/hosted-mode.md` +110 (new), `CLAUDE.md` +14/−1 recording the SQLite addition to the Project Overview and the new `identity`/`db`/`auth` invariants under Architecture Notes. All four repo-root gates green (below); `make docker-build` green at `f916444` per `task-28-gate.log:594` `EXIT_CODE(make docker-build)=0`. **`docs/hosted-mode.md:107-110` contradicts the shipped code** — see Important finding I-1. |
| 30 | per-invocation `gitx.Spec` secrets | **ADHERENT** | `gitx/spec.go:34-41` adds `Spec.Secrets` with the layering documented; `gitx/exec.go:114-119` merges `Options.Secrets` with `Spec.Secrets` per invocation. `gitx/spec_secrets_test.go` (+88) asserts both the spec-scoped and the runner-wide secret are redacted. `provider/github/client.go` +8 and `provider/gitlab/client.go` +6 declare the base64 `Authorization: Basic` blob as a secret (commit `55e5af1`), with guard tests at `provider/github/authorize_git_spec_secret_test.go` (+63) and `provider/gitlab/authorize_git_spec_secret_test.go` (+64). |

**Verdict count: 29 of 29 audited tasks adherent (1–28 and 30). 0 skipped, 0 stubbed, 0 partial.**

No task was implemented in a weaker form than specified. The only specified-value drift found
is `loadProviders`'s third return (Task 2), which is an unexported helper whose extra return is
itself mandated by the plan's `IgnoredProviderVars` field.

---

## Requirement #1: standalone equivalence

This was audited as a discipline question, not just a behaviour question.

**Behaviour.** With no new environment variable set: `config.go:225-229` defaults the mode to
standalone; `config.go:248-251`'s hosted block is never entered, so `CONVERGE_SECRET_KEY` is
never read and `DatabasePath` stays empty; `app_test.go:378`
`TestStandaloneCreatesNoDatabaseFile` asserts no file is created. `router.go:130` registers no
hosted route and `:163` composes no middleware, so standalone's request path gains zero
comparisons. `health.go:25` omits the `database` key entirely, keeping `/healthz` byte-identical.
`api/isolation_test.go:377` `TestStandaloneRouterIsUnchanged` is the direct guard.

**Discipline — the load-bearing check.** I reviewed every removed line in every pre-existing
test file in `7ae00b9..f916444`:

- `internal/api/api_test.go`, `internal/review/harness_test.go`, `resolve_test.go`,
  `integration_test.go`, `internal/mirror/objects_test.go`,
  `internal/workspace/manager_test.go`, `internal/mirror/cache_test.go`,
  `internal/session/store_test.go`, `internal/review/service_test.go`,
  `cmd/converge-cli/main_test.go` — **every** edit is a call-site signature substitution
  (`registry` → `provider.NewStaticResolver(registry)`, `store.Get(id)` →
  `store.Get(id, identity.Standalone())`, `cache.Ensure(ctx, p, repo)` →
  `cache.Ensure(ctx, mirror.RootNamespace(), p, repo)`, `svc.Finish(ctx, id)` →
  `svc.Finish(ctx, id, identity.Standalone())`). No assertion was added, removed, weakened,
  or retargeted inside any pre-existing test function body.
- `internal/config/config_test.go` is +143/−0 — strictly appended, as the plan demanded.
- The backend's new test functions are all genuinely new files or appended functions.

So the equivalence claim is not propped up by edited assertions. That is the strongest single
piece of evidence on this branch.

**One frontend qualification (M-1 below).** `apps/frontend/src/__tests__/App.test.tsx`'s three
pre-existing tests *were* restructured: they became `async`, several `getByRole` calls became
`await findByRole`, MSW now serves `/api/auth/mode`, and a `freshApp()` dynamic-import helper
was added to defeat `App`'s module-level `QueryClient` singleton under `staleTime: Infinity`.
No assertion was added to those bodies, and the restructuring is forced by `ModeGate`'s
design (Task 22 item 3 explicitly specifies a loading state before the mode resolves). But it
does mean standalone equivalence is byte-identical at the *server* level and
render-equivalent-after-one-async-tick at the *client* level, not literally unchanged. Worth
stating plainly rather than leaving implicit.

---

## Dependency direction

Verified with `go list -deps`, per brief.

```
./internal/identity  → identity                                        (stdlib leaf ✓)
./internal/db        → db                                              (stdlib leaf ✓)
./internal/gitx      → gitx                                            (imports nothing ✓)
./internal/auth      → config, identity, gitx, provider, provider/github, provider/gitlab
./internal/review    → gitx, diff, identity, provider, mirror, workspace, session
./internal/session   → gitx, diff, identity, workspace
```

- `auth` does **not** import `review` or `session` ✓
- `review` does **not** import `auth` ✓ (the deletion cascade crosses via the `auth.Purger`
  and `auth.ProviderUsage` interfaces, wired in `internal/app`)
- `gitx` appears in `auth`'s closure only transitively through `provider`, which the plan's
  allow-list permits.
- `auth` does not import `internal/db` in production at all — only its `_test.go` files do,
  and `store.go` takes a bare `*sql.DB`. This is residual #7, already adjudicated; the
  `CLAUDE.md` wording states a *permission*, not a crossed boundary.

---

## Findings

### Critical

None.

### Important

**I-1 — `docs/hosted-mode.md:107-110` contradicts the shipped `converge-cli` behaviour.**

The document states:

> It shares `internal/app`'s wiring code with the server binary, so `CONVERGE_MODE=hosted`
> present in the CLI's own environment would still cause it to attempt to open the database —
> operators should not set `CONVERGE_MODE=hosted` in a shell where `converge-cli` runs.

That is no longer true. Commit `fc657d4` pinned the mode at `cmd/converge-cli/main.go:80`:

```go
application, err := newApp(ctx, append(os.Environ(), "CONVERGE_MODE=standalone"))
```

and `cmd/converge-cli/main_test.go:164`
`TestRunStaysStandaloneWhenHostedModeLeaksIntoEnvironment` asserts the database file is never
created even with `CONVERGE_MODE=hosted`, `CONVERGE_SECRET_KEY`, and `CONVERGE_DATABASE_PATH`
all set. The doc now documents a hazard the code removed, and issues an operator warning that
is both unnecessary and misleading about where the safety boundary is.

Residual #8 asked specifically to "confirm the documented behaviour matches the code". It does
not. The paragraph's first two sentences (lines 103-107, "never opens the hosted database…
always constructs its scope as `identity.Standalone()`") are correct and should stay; the
`would still cause it to attempt to open the database` clause and the operator warning should
be replaced with the pinning behaviour and a pointer to the test that holds it.

Severity rationale: no code defect, but this is the one place an operator would look before
running the CLI inside a hosted container, and it tells them the opposite of the truth. It is
also the *only* inconsistency I found between the branch's docs and its code.

### Minor

**M-1 — pre-existing frontend `App.test.tsx` tests were restructured, not merely rewired.**
`apps/frontend/src/__tests__/App.test.tsx` — three pre-existing tests became `async`, gained
MSW handlers for `/api/auth/mode`, and route through a new `freshApp()` dynamic-import helper.
No assertion was added or changed, and the cause (ModeGate's async gate) is plan-specified, so
this is not a discipline breach. Recording it because the equivalence claim reads as "nothing
changed" and at the client level something did.

**M-2 — commit `5fcb4cf`'s message still carries the superseded Task 30 framing.** The body
reads "…no hosted token was in that list and one reaching git's stderr was logged in clear",
which is the stronger claim the later Opus review retired. The correction is properly recorded
in `.superpowers/sdd/plan/residuals.md` and referenced from `f916444`'s message, and commit
messages are immutable history, so there is nothing to fix — but per the brief's instruction to
name any artifact still asserting it, here it is. No comment, doc, or code on the branch
asserts the stronger claim; only this one commit message does.

**M-3 — every checkbox in `plan.md` is still `- [ ]`.** All 174 task-step checkboxes are
unchecked across a branch where all 29 audited tasks are demonstrably complete. The execution
record lives in `.superpowers/sdd/plan/progress.md` and the per-task reports instead. Harmless
for correctness; it does mean `plan.md` read alone implies nothing was done, which is the
opposite failure mode from the one this audit exists to catch.

**M-4 — `f916444`'s commit message claims it "produces real, verbatim gate output
(apps/.superpowers/sdd/plan/task-28-gate.log)" but the commit changes only `CLAUDE.md`.** The
log does exist and is genuine (I read it: `task-28-gate.log:594`
`EXIT_CODE(make docker-build)=0`), but it lives in a gitignored directory, so the gate evidence
is not actually committed with the branch and will not survive a fresh clone. Also note the
path in the message (`apps/.superpowers/…`) does not match the real location
(`.superpowers/…`).

---

## Adjudicated items re-verified (not findings)

Each approved deviation was independently confirmed in the code, and each was correctly
characterised:

| Item | Confirmed at |
|---|---|
| #1 `schema_migrations` created by the runner | `internal/db/db.go:85`; omission documented `migrations/0001_init.sql:6` |
| #2 `auth.NewID` is 16 hex chars | `internal/auth/model.go:24,31` (`idBytes = 8`) |
| #3 `deleteReview` inspects visibility in the handler | `internal/api/reviews.go` (route at `router.go:126`) |
| #4 `sessionFor` consults `Corrupted` only when unscoped | `internal/api/reviews.go` |
| #5 `ProviderPatch.Validate` defaults to `false` | `internal/api/settings_providers.go:141`, rationale `:127-130`; POST defaults `true` at `:106-109` |
| #6 `auth.Service` gains `ProviderUsage` | `internal/auth/service.go` `ServiceDeps.Usage`; implemented `internal/review/service.go` `ProviderInUse` |
| #7 `PurgeNamespace` refuses the root namespace | `internal/mirror/cache.go:97-101,108-112`, rationale `:104-107` |

**My judgements on the open residuals** (offered as judgements, per the brief, not as missed
requirements):

- **Residual #1 (credential injection untested because `fake.AuthorizeGit` is a no-op).** The
  ruling holds, and Task 30 strengthened it: `gitx/exec.go` now logs only category, repo,
  session, exit code, duration, and redacted stderr, and `Spec.Secrets` gives any future
  per-invocation secret a redaction path. The residual cost — "if future code ever logs
  `Spec.Args` or `Spec.Env`, no test catches it" — is real but is a *future-regression* risk,
  not a current gap, and the cheapest guard is residual #2's `LogValue()`. I would ticket it,
  not block on it.
- **Residual #7 (`CLAUDE.md` says `auth` may import `db`).** I would tighten the wording to
  describe what the code does: `auth` takes a bare `*sql.DB` and does not import
  `internal/db`. As written, the invariant permits a coupling the code deliberately avoids,
  which makes the doc weaker than the design. Minor, and a doc edit.
- **Residual #8 (CLI honours `CONVERGE_MODE`).** My view differs from the recorded ruling,
  because the code changed after the ruling was made: the CLI no longer honours it
  (`main.go:80`), so the residual is *closed in code* and *stale in docs*. That is I-1.
- **The unreproduced `internal/auth` flake.** I ran the full suite twice more (`make test` and
  `make test-integration`, `-race -count=1`); `internal/auth` passed in 6.06s and 6.34s. I
  looked for a mechanism and found none of the usual candidates: `Throttle` takes an injected
  `now func() time.Time` rather than reading a shared clock, `store_test.go` uses per-test
  temporary databases, and the `32f668e` fix round made the failure counter's
  read-modify-write atomic — which is exactly the shape of bug that produces a
  load-dependent, unreproducible flake, and it is plausibly the original cause. Combined with
  ~22 consecutive clean runs since, **I consider the evidence sufficient to retire it**, with
  the named residual risk being that the retirement rests on a causal inference about
  `32f668e` rather than on a reproduction.

---

## Gate results

All gates run in the foreground, one at a time, from the worktree root.

| Gate | Result | Output |
|---|---|---|
| `make lint` | **PASS** | `go vet ./...` clean; `golangci-lint run` → `0 issues.`; `eslint .` clean; `prettier --check .` → `All matched files use Prettier code style!` |
| `make test` | **PASS** | Backend `go test -race -count=1 ./...` — 22 packages `ok`, 0 failures (`internal/auth` 6.059s, `cmd/converge` 41.272s, `internal/review` 9.691s). Frontend `vitest run` — **42 files passed, 337 tests passed**, 16.64s. |
| `make test-integration` | **PASS** | `go test -race -count=1 -tags integration ./...` — 22 packages `ok`, 0 failures (`internal/review` 12.244s with the hosted integration tests in play). |
| `make build` | **PASS** | Frontend built into `apps/backend/internal/ui/dist`; `tools/build-backend.sh` produced `converge` and `converge-cli` for linux/amd64 and linux/arm64 → `dist/converge-0.0.0-f916444-linux-{amd64,arm64}.tar.gz`. Only a pre-existing chunk-size warning (`emacs-lisp` 790 kB, `cpp` 785 kB), unrelated to this branch. |
| `make docker-build` | **PASS (read from log, not re-run)** | `.superpowers/sdd/plan/task-28-gate.log:470-594` at `f916444`: full buildx output ending `#29 importing to docker` and `EXIT_CODE(make docker-build)=0`. |

`git status --porcelain` after the gates shows no tracked modification — only the untracked
`docs/tasks/task-005-hosted-multi-user-mode/audit-frontend.md` from a concurrent reviewer.

---

## Overall assessment

- **Plan adherence:** FULL — 29/29 audited tasks adherent, with file:line evidence for each.
- **Test fidelity:** every one of the 177 `Test*` identifiers named in the plan exists; every
  one of the 59 numbered frontend assertions exists; the two deliberately-skipped tests are
  un-skipped and assert substantively; Task 5's deliberate `argon2.IDKey` bypass survives.
- **Standalone equivalence:** held, and held honestly — no pre-existing backend test function
  body was edited beyond mechanical signature churn.
- **Gates:** 5/5 green.
- **Recommendation:** **READY_TO_MERGE after I-1.** The single Important finding is a stale
  paragraph in `docs/hosted-mode.md`; it is a doc edit, not a code change, and nothing else on
  the branch blocks.

## Action items

1. **(Important)** Rewrite `docs/hosted-mode.md:107-110` to describe the mode pinning at
   `cmd/converge-cli/main.go:80` and cite
   `TestRunStaysStandaloneWhenHostedModeLeaksIntoEnvironment`; drop the
   "would still cause it to attempt to open the database" clause and the operator warning.
   Mark residual #8 closed.
2. **(Minor)** Tick the completed checkboxes in `plan.md`, or add a one-line header pointing at
   `.superpowers/sdd/plan/progress.md` as the execution record.
3. **(Minor)** Tighten `CLAUDE.md`'s `internal/auth` invariant to state that `auth` takes a
   bare `*sql.DB` and does not import `internal/db` (residual #7).
4. **(Minor, optional)** Either commit the gate log outside `.superpowers/` or stop citing it
   as committed evidence in commit messages; and fix the `apps/.superpowers/…` path in
   `f916444`'s message if the history is ever rewritten.
5. **(Ticket, not this branch)** Add `LogValue()` to `gitx.ExitError.Result` (residual #2) —
   it is the cheapest guard against the future-regression risk residual #1 leaves open.

---

# Reviewer report — backend DOM/SUB/SEC checklist

# Backend Audit — task-005 hosted multi-user mode

- **Worktree:** `.worktrees/task-005-hosted-multi-user-mode`
- **Range:** `7ae00b9..f916444` (50 commits)
- **Guidelines source:** `.claude/skills/backend-dev-guidelines/resources/`
- **Date:** 2026-09-11
- **Build:** PASS (not re-run; read from captured gate log)
- **Tests:** PASS (not re-run; read from captured gate log)
- **Overall:** NEEDS-WORK (one guideline FAIL; no merge-blocking defect)

Documented project deviations from the guidelines skill (`CLAUDE.md`) — `session.json` as
entity, `log/slog` injected through constructors, pipeline steps as plain functions — are
excluded from the findings below, as instructed.

## Build & Test Results

Gates were **not** executed by this audit (a concurrent reviewer held the worktree).
Read verbatim from `.superpowers/sdd/plan/task-28-gate.log`:

| Gate | Exit code | Evidence |
|---|---|---|
| `make lint` | 0 | task-28-gate.log:15 |
| `make test` | 0 | task-28-gate.log:58 |
| `make test-integration` | 0 | task-28-gate.log:83 |
| `make build` | 0 | task-28-gate.log:445, :469 |
| `make docker-build` | 0 | task-28-gate.log:594 |

All 22 backend packages report `ok`; `internal/provider/fake` is `[no test files]`
(task-28-gate.log:18-39, :61-82, :447-468). Zero failures, zero skips recorded.

The only command this audit executed was the read-only
`go list -deps ./internal/<pkg>` sweep used for the dependency-direction check below.

## Package Classification (Phase 2)

| Package | Classification | Note |
|---|---|---|
| `internal/auth` | Domain (`model.go` present) | full DOM checklist |
| `internal/session` | Domain (`model.go` present, pre-existing) | modified; DOM checked for regressions |
| `internal/identity` | Support (leaf value type) | `scope.go` only |
| `internal/db` | Support (persistence leaf) | connection + migrations |
| `internal/api` | Transport | handler-layer DOM items apply |
| `internal/provider`, `provider/{github,gitlab,fake}` | Support | `resolver.go` added |
| `internal/mirror`, `internal/review`, `internal/gitx`, `internal/config`, `internal/app` | Support | modified |

## Domain Checklist — `internal/auth`

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| DOM-01 | `builder.go` exists with `NewBuilder()` + `Build()` validation | **FAIL** | No `builder.go` in `internal/auth/` (directory listing). `model.go` defines two models (`User` at `internal/auth/model.go:94`, `UserProvider` at `internal/auth/model.go:159`); construction goes through `NewUser` (`model.go:104`) and `NewUserProvider` (`model.go:185`). Guideline: `ai-guidance.md:170`, `scaffolding-checklist.md:139`. Contrast `internal/session/builder.go:18`, which does have `NewBuilder()`. |
| DOM-02 | `ToEntity()` on model | N/A | No GORM entity tier (documented `CLAUDE.md` deviation). The persistence boundary is `internal/auth/store.go`, which scans directly into unexported model fields (`store.go:70`, `store.go:214`). |
| DOM-03 | `Make(Entity)` | N/A | Same as DOM-02; `scanUser` (`store.go:70`) and `scanUserProvider` (`store.go:214`) fill the role. |
| DOM-04/05 | `Transform` / `TransformSlice` in `rest.go` | PASS (equivalent) | No api2go tier; the transport mapping lives in `internal/api`: `userResource` (`internal/api/auth.go:56`), `userProviderResource` (`internal/api/settings_providers.go:55`). List handlers loop over the single-resource mapper rather than inlining field access (`settings_providers.go:87-90`, `auth.go` n/a). |
| DOM-06 | Processor takes an interface logger, not a concrete one | PASS | `ServiceDeps.Log *slog.Logger` (`internal/auth/service.go:63`) injected via `NewService` (`service.go:78`); no package-level logger, no `slog.Default()` anywhere in `internal/auth`. |
| DOM-07 | Handlers pass the injected logger | PASS | Every handler uses `s.deps.Log` (`internal/api/auth.go:73,91,109,132,151`; `settings_providers.go:77,84,92,123,153`). No `slog.Default()` in `internal/api`. |
| DOM-08 | POST/PATCH use a typed input handler, not raw body reads | PASS | `jsonapi.Decode[credentialAttributes]` (`auth.go:78`, `auth.go:96`), `[passwordAttributes]` (`auth.go:156`), `[accountDeletionAttributes]` (`auth.go:172`), `[createUserProviderAttributes]` (`settings_providers.go:97`), `[patchUserProviderAttributes]` (`settings_providers.go:132`). |
| DOM-09 | Transform/decode errors handled, never discarded | PASS | Every `jsonapi.Decode` call checks `err` and returns (`auth.go:79-82,97-100,157-160,173-176`; `settings_providers.go:98-101,133-136`). Zero `_, _ :=` or `_ =` on a decode in `internal/api`. |
| DOM-10 | Providers use lazy evaluation | N/A (adapted) | No `database.Query`/`FixedProvider` abstraction in this project. The analogue — lazy per-scope provider resolution — is `provider.Resolver` (`internal/provider/resolver.go:20`) with `auth.ProviderResolver.Resolve` (`internal/auth/resolver.go:63`) building on demand behind a TTL cache. See Important finding 3 on that cache. |
| DOM-11 | No `os.Getenv()` in handlers | PASS | `grep -rn "os.Getenv" internal/api/` → zero matches. (The only `os.Getenv` in the module is `PATH` pass-through at `internal/gitx/exec.go:87`.) |
| DOM-12 | No cross-domain logic in handlers | PASS | Handlers call exactly one service method plus the response writer. The only cross-store orchestration — account deletion cascading into review sessions and mirrors — is inverted behind interfaces in the service tier: `auth.Purger` (`internal/auth/service.go:41`) and `auth.ProviderUsage` (`service.go:47`), implemented at `internal/review/service.go:647` and `:665`. |
| DOM-13 | Handlers don't call stores/providers directly | PASS | `internal/api` imports neither `internal/db` nor `database/sql`: `grep -rn "database/sql\|\*sql\." internal/api/*.go` (non-test) → zero matches. Provider access goes through `s.deps.Providers.Resolve` (`internal/api/providers.go:23`, `repositories.go:42`), never a registry literal. |
| DOM-14 | No direct writes in handlers | PASS | Zero `db.Create`/`db.Save`/`db.Exec` in `internal/api`; every write goes handler → `auth.Service` → `auth.Store` (e.g. `settings_providers.go:110` → `providers.go:39` → `store.go:233`). |
| DOM-15 | Write tier exists | PASS (equivalent) | `internal/auth/store.go` is the administrator-equivalent: `CreateUser:41`, `SetPasswordHash:98`, `DeleteUser:110`, `CreateLoginSession:137`, `CreateUserProvider:233`, `UpdateUserProvider:299`, `DeleteUserProvider:318`. Called only from `service.go`/`providers.go`, never from `internal/api`. |
| DOM-16 | Domain error → HTTP status mapping | PASS | `(*auth.Error).Status()` (`internal/auth/errors.go:63-78`) maps every code; `api.classify` (`internal/api/errors.go:32-71`) recognises it with one `errors.As` arm at `errors.go:33-36`. Verbatim against the plan: `UNAUTHENTICATED`/`INVALID_CREDENTIALS` 401 (`errors.go:65`), `FORBIDDEN` 403 (`:67`), `USERNAME_TAKEN`/`PROVIDER_SLUG_TAKEN`/`PROVIDER_IN_USE` 409 (`:69`), `INVALID_USERNAME`/`WEAK_PASSWORD`/`PROVIDER_UNAUTHORIZED` 422 (`:71`), `ACCOUNT_LOCKED` 429 (`:73`), default 500 (`:75`). All ten codes declared at `errors.go:21-31`. |
| DOM-17 | JSON:API identity on REST models | PASS | `jsonapi.Resource{Type:..., ID:...}` is the project's shape; set at `internal/api/auth.go:57` (`typeUsers`), `auth.go:68` (`typeModes`), `settings_providers.go:56` (`typeUserProviders`), `reviews.go:72` (`reviews`). Type constants declared verbatim at `auth.go:16-22` and `settings_providers.go:13`. |
| DOM-18 | Request models are flat | PASS | `credentialAttributes` (`auth.go:32`), `passwordAttributes` (`auth.go:45`), `accountDeletionAttributes` (`auth.go:50`), `createUserProviderAttributes` (`settings_providers.go:30`), `patchUserProviderAttributes` (`settings_providers.go:47`) are all flat attribute structs — the `data/type/attributes` envelope is peeled by `jsonapi.Decode`, not by the models. |
| DOM-19 | Table-driven tests | **WARN** | Present: `internal/auth/crypt_test.go`, `internal/auth/password_test.go`, `internal/api/auth_test.go`, `internal/api/authmw_test.go`, `internal/api/settings_providers_test.go`, `internal/identity/scope_test.go`. Absent (neither `[]struct` table nor `t.Run`): `internal/auth/store_test.go` (17 top-level `Test` funcs), `service_test.go` (15), `providers_test.go` (16), `throttle_test.go` (10), `model_test.go` (7), `resolver_test.go` (9), `internal/db/db_test.go` (6). `testing-guide.md:39` says "*Prefer* table-driven tests" — a preference, so WARN rather than FAIL. |
| DOM-IMM | All mutations via builders returning new instances (`patterns-functional.md:13`) | **FAIL** | `Service.UpdateProvider` mutates model fields in place on a local copy: `row.displayName` (`internal/auth/providers.go:105`), `row.kind` (`:108`), `row.baseURL` (`:113`), `row.tokenCiphertext`/`row.tokenNonce` (`:148`), `row.tokenLast4`/`row.tokenSetAt` (`:149`), `row.updatedAt` (`:151`). The caller's value is unaffected (value semantics), so this is not a correctness bug — but it is field assignment, not builder-mediated construction. Same root cause as DOM-01. |

## Domain Checklist — `internal/session` (modified)

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| DOM-01 | `builder.go` | PASS | `internal/session/builder.go:18` `func NewBuilder() *Builder`; `SetOwner` threaded at `internal/review/service.go:162`. |
| DOM-16 | Scoped not-found → 404, never 403 | PASS | `Store.Get` folds "absent" and "owned by someone else" into the same `false` (`internal/session/store.go:186-195`), which flows to the existing `session.ErrNotFound` → 404 arm (`internal/api/errors.go:61-62`). |
| DOM-19 | Table-driven tests | WARN | `internal/session/store_test.go` is one-func-per-case like the `auth` files. Same preference-not-mandate ruling. |

## Sub-Domain Checklist — `internal/api` hosted endpoints

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| SUB-01 | Business logic not in handler | PASS | Handlers are decode → one service call → write. Longest is `currentUser` at 15 lines with two service calls and no branching logic (`internal/api/auth.go:138-153`). |
| SUB-02 | No writes in `resource.go`-equivalents | PASS | Zero `sql`/`db` references in `internal/api` (non-test). |
| SUB-03 | Typed input handler for POST/PATCH | PASS | See DOM-08. |
| SUB-04 | No manual JSON parsing | PASS | `grep -rn "json.NewDecoder\|json.Unmarshal\|io.ReadAll" internal/api/*.go` (non-test) → zero matches. The one `encoding/json` use is response encoding at `internal/api/health.go:33`. |

## Security Review

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| SEC-01 | All SQL parameterised | PASS | Every statement in `internal/auth/store.go` uses `?` placeholders with args (`:49, :60, :87, :93, :100, :111, :128, :139, :151, :168, :177, :187, :198, :242, :253, :268, :293, :301, :319, :337, :350, :367, :390, :407, :421, :431`). The only string concatenation into a query is two **compile-time constants**: `userColumns` (`store.go:83`) and `userProviderColumns` (`store.go:212`) — no value is ever concatenated. `internal/db/db.go` likewise: the one non-parameterised `Exec` is the embedded migration body (`db.go:172`, with the rationale at `:170-171`); the bookkeeping statements at `:85, :122, :181` are constants/parameterised. |
| SEC-01b | `store.go` is the *only* file in the repository containing SQL | **FAIL (claim)** | `internal/db/db.go:85` (`CREATE TABLE schema_migrations`), `:122` (`SELECT version FROM schema_migrations`), `:181` (`INSERT INTO schema_migrations`), plus the DDL file `internal/db/migrations/0001_init.sql`. The *substance* of the constraint holds (all domain SQL in one file, nothing value-concatenated); the exclusivity sentence asserted at `internal/auth/store.go:13` does not. See Important finding 2. |
| SEC-02 | Every git call goes through `gitx.Runner` with an arg slice, never a shell | PASS | `exec.CommandContext(ctx, r.gitPath, args...)` — single binary path plus a slice, no `sh -c` (`internal/gitx/exec.go:135`). `grep -rn "exec.Command"` finds no shell invocation in production code. Call sites build `gitx.Spec{Args: []string{...}}` (`internal/mirror/cache.go:146, :152, :177`). |
| SEC-02b | Credentials reach git only via `GIT_CONFIG_COUNT`/`KEY_0`/`VALUE_0`, never argv, never a stored remote URL | PASS | `gitx.CredentialEnv` returns exactly those three env entries (`internal/gitx/credentials.go:28-32`); the only callers are `github.Client.AuthorizeGit` (`internal/provider/github/client.go:184`) and `gitlab.Client.AuthorizeGit` (`internal/provider/gitlab/client.go:233`), both appending to `spec.Env` only (`client.go:188` / `:237`). The clone argv carries `p.CloneURL(repo)` — `repo.CloneURL()` verbatim from the provider API (`github/client.go:180`, `gitlab/client.go:230`), with no userinfo injected. Pinned by `internal/provider/github/client_test.go:196-205` and `gitlab/client_test.go:178`. |
| SEC-03 | Argon2id exact parameters | PASS | `argonTime=1` (`internal/auth/password.go:28`), `argonMemory=64*1024` i.e. 65536 KiB (`:29`), `argonThreads=4` (`:30`), `argonKeyLen=32` (`:31`), `argonSaltLen=16` (`:32`), `argon2.Version` 19 (`:33`). PHC encoding at `password.go:100-105`. Verification reads `m`/`t`/`p` back **from the stored string**, not from the constants: `decodePHC` at `password.go:107` feeds `p.time, p.memory, p.threads` into `argon2.IDKey` at `password.go:85`. Bounds-checked against tamper ceilings (`password.go:44-46`, enforced `:141-143`). Constant-time compare at `password.go:86`. |
| SEC-04 | Provider tokens AES-256-GCM, per-record nonce, AAD binding to `user_id` + provider row id | PASS | Key length 32 enforced (`internal/auth/crypt.go:17`, `:34-36`); `aes.NewCipher` + `cipher.NewGCM` (`crypt.go:37-45`). Fresh nonce from `crypto/rand` per `Seal` call (`crypt.go:66-69`). AAD is `userID \x00 providerID` with an explicit NUL separator against prefix ambiguity (`crypt.go:54-60`), applied on both `Seal` (`:71`) and `Open` (`:82`). Sealed under the row's own freshly generated id (`providers.go:59-65`). Ciphertext/nonce stored in the row's own columns (`store.go:253-255`). |
| SEC-05 | Session cookie named `converge_session`, base64url of 32 random bytes, only SHA-256 persisted | PASS | Name constant `"converge_session"` (`internal/api/authctx.go:13`), used at `authctx.go:71` and `:85`. `tokenBytes = 32` (`internal/auth/service.go:33`); `newToken` reads 32 bytes from `crypto/rand` and returns `base64.RawURLEncoding` (`service.go:105-113`). Only the hash is persisted: `startSession` passes `hash` to `NewLoginSession` (`service.go:189-193`), and `CreateLoginSession` inserts `ls.tokenHash` (`store.go:140`). `LoginSession` has no plaintext field (`model.go:151-158`). Cookie flags: `HttpOnly: true`, `SameSite=Lax`, `Secure: s.secure(r)` (`authctx.go:74-79`), with `secure` honouring TLS or `CONVERGE_SECURE_COOKIES` (`authctx.go:65-67`). |
| SEC-06 | No password, token, cookie value, or master key is ever logged | PASS | Adversarial sweep, four layers: (a) `config.Secret` has `String()`, `MarshalJSON()`, and `LogValue()` all returning `[redacted]` (`internal/config/secret.go:19-25`); (b) the `redactingHandler` scrubs `cfg.Secrets()` from every attribute, recursing into groups (`internal/app/app.go:158-172`, installed at `:199`), and `loggableURL` strips userinfo before a base URL is logged (`app.go:181-187`); (c) `gitx.Run` logs only category, repo, session, exit code, duration, outcome, and `Redact`-ed stderr — `Spec.Args` and `Spec.Env` appear in no log call (`internal/gitx/exec.go:172-196`), and the redaction set is the union of runner-wide and per-invocation secrets (`secretsFor`, `exec.go:113-121`); (d) `Secret.Reveal()` has exactly six production call sites, all of them terminal sinks — the GCM key (`auth/crypt.go:31`), two HTTP headers (`github/client.go:83`, `gitlab/client.go:58`), two `CredentialEnv` calls (`github/client.go:184`, `gitlab/client.go:233`), and the `spec.Secrets` declarations (`github/client.go:196`, `gitlab/client.go:243`); the redaction list builder (`config/config.go:109,116`) is the only other. Every log call in `internal/auth` carries `user_id` and metadata only, never a credential: `service.go:155,176,182,210,297,308` and `providers.go:82,156,179`. The failed-login event deliberately omits even the attempted username (`service.go:177-180`). `grep` for a log call whose args mention token/password/secret/cookie/credential returns only `slog.Bool("token_rotated", ...)` (`providers.go:157`) and the redaction plumbing itself. **No path found.** Nearest miss, for the record: `auth.ProviderInput.Token` and `ProviderPatch.Token` are plain `string`/`*string` (`providers.go:19`, `:30`) rather than `config.Secret`, so a future `slog.Any("input", in)` would not be caught by `Secret.LogValue`; no such call exists today. |
| SEC-07 | Dependency direction | PASS | `go list -deps` (the one command this audit ran): `internal/identity` → no module imports (stdlib leaf). `internal/db` → no module imports (stdlib + `modernc.org/sqlite` leaf). `internal/auth` → `config`, `identity`, `gitx`, `provider`, `provider/github`, `provider/gitlab` — **no `review`, no `session`** ✓. `internal/review` → `gitx`, `diff`, `identity`, `provider`, `mirror`, `workspace`, `session` — **no `auth`** ✓. `internal/provider` → `gitx`, `identity`. `internal/gitx` → nothing. The `api → review → {...} → gitx` direction holds. |
| SEC-08 | `CGO_ENABLED=0` builds; `modernc.org/sqlite` mandatory, `mattn/go-sqlite3` forbidden | PASS | `modernc.org/sqlite v1.58.0` as a direct require (`apps/backend/go.mod:9`), imported blank with the rationale at `internal/db/db.go:22-25`. `grep mattn go.mod` finds only `go-colorable`, `go-isatty`, `go-runewidth` (indirect, unrelated) — **no `mattn/go-sqlite3`** in `go.mod` or `go.sum`. `make build` (which runs the `CGO_ENABLED=0` build) exited 0 (task-28-gate.log:445). |
| SEC-09 | Multi-tenant isolation: no path to another user's session, mirror, or provider config | PASS | **Scope is unforgeable:** `identity.Scope.userID` is unexported with no setter, so the only constructors are `Standalone()` and `ForUser()` (`internal/identity/scope.go:14-23`), and `ForUser` is reached from the request path only inside the authenticate middleware (`internal/api/authmw.go:73`). **Sessions:** `Get` (`session/store.go:186-195`) and `List` (`:211-221`) gate on `scope.Matches(sess.Owner())`; `Finish` routes through `Get` (`:239`). Owner is assigned once, from the creating scope (`review/service.go:162`). Background builds re-derive scope from the persisted owner, not a stale request scope (`scopeOf`, `review/service.go:422-428`, used at `:438`). `Files`/`FileDiff`/`CombinedDiffPath` each re-check through the scoped `Get` (`review/service.go:593, 605, 628`). **Provider configs:** every read and write carries `user_id` in the `WHERE` clause (`auth/store.go:242, 268, 293, 301, 319, 337`), and `UpdateUserProvider`/`DeleteUserProvider` return `ErrNotFound` on `RowsAffected()==0` (`:311-313`, `:327-329`). **Mirrors:** `NamespaceFor` yields `users/<user-id>` (`mirror/cache.go:42-50, :56-61`), validated against the exact 16-hex shape `auth.NewID` produces (`cache.go:26`) rather than trusted, and `PurgeNamespace` refuses the root namespace so a zero-value `Namespace` cannot wipe every user's cache (`cache.go:105-116`). **Resolver:** keyed per user, and an unscoped resolve is a hard error rather than "everything" (`auth/resolver.go:63-70`). |
| SEC-10 | 403-vs-404 does not disclose existence | PASS | `auth.ErrNotFound` is documented and used as the single answer for both unknown and foreign rows (`auth/errors.go:41-44`), mapped to 404 at `api/errors.go:61-62`. `session.Store.Get` folds both cases into `false` (`session/store.go:174-195`). The one place where a 500-vs-404 oracle could arise is handled explicitly: `Corrupted(id)` is consulted **only when the scope is unscoped**, so a hosted caller gets 404 either way (`api/reviews.go:128-138`). `deleteReview` preserves the pre-hosted unconditional 204 for standalone while answering 404 for an invisible id in hosted mode (`api/reviews.go:171-191`). |
| SEC-11 | Login does not leak username existence | PASS | Unknown username verifies against a package `dummyHash` built once at init from 32 random bytes, so the dominant Argon2id cost is paid on both paths (`auth/password.go:177-197`, used at `auth/service.go:168-173`). Both paths return the byte-identical `invalidCredentialsMessage` (`service.go:51`, returned at `:183`). The lockout message is identical for a username lock and an IP lock (`auth/throttle.go:34-36`). |
| SEC-12 | CSRF / cross-site defence | PASS | `originGuard` rejects any non-GET/HEAD `/api/` request that is neither `Sec-Fetch-Site: same-origin` nor `Origin`-host-matching, with `FORBIDDEN` 403 (`api/authmw.go:40-61`); it wraps `authenticate` so it runs first (`api/router.go:163-169`). Combined with `SameSite=Lax` (`authctx.go:76`). Fails closed: a request bearing neither header is rejected. |
| SEC-13 | Throttle key cannot be forged | PASS | `X-Forwarded-For` is honoured only when the operator declared a trusted proxy, and then only the last hop; otherwise `RemoteAddr` (`api/authctx.go:98-111`). Counters are persisted so a restart does not clear a lockout in progress (`auth/store.go:365-373`, read at `:348`), and the read-modify-write is transactional (`UpdateAttempt`, `store.go:382-417`). |
| SEC-14 | No hardcoded secrets | PASS | `CONVERGE_SECRET_KEY` is required in hosted mode with no default and is length-validated (`config/config.go:253-266`). The only credential-shaped literals are error-code strings with `#nosec G101` justifications (`auth/errors.go:23`) and test fixtures. No default password, no fallback key. |
| SEC-15 | Expiry enforced on read, not only by the sweeper | PASS | `Authenticate` rejects and deletes a row past absolute or idle expiry before honouring it (`auth/service.go:245-252`); the sweeper is explicitly documented as housekeeping rather than enforcement (`auth/sweep.go:10-13`). |
| SEC-16 | Password change revokes other sessions | PASS | `DeleteOtherLoginSessions(ctx, userID, keep)` (`auth/service.go:292`) → `DELETE ... WHERE user_id = ? AND token_hash != ?` (`auth/store.go:187`), with `keep` sourced from the calling session's hash via `tokenHashFrom` (`api/auth.go:163`, `api/authctx.go:39-44`). |
| SEC-17 | `ExitError` does not carry stderr into an error string | PASS | `(*ExitError).Error()` emits only category and exit code (`internal/gitx/spec.go:58-60`). Repo-wide grep for `.Stderr` outside `internal/gitx` finds **no production caller** — only `os.Stderr` logger wiring and the two `authorize_git_secret_test.go` precondition assertions. |

## Findings

### Critical

None.

### Important

**I-1. `docs/hosted-mode.md` contradicts the shipped code (stale doc).**
`docs/hosted-mode.md:106-110` states: "It shares `internal/app`'s wiring code with the
server binary, so `CONVERGE_MODE=hosted` present in the CLI's own environment would still
cause it to attempt to open the database — operators should not set `CONVERGE_MODE=hosted`
in a shell where `converge-cli` runs."

The code no longer behaves that way. `cmd/converge-cli/main.go:80` appends
`"CONVERGE_MODE=standalone"` to `os.Environ()` so a later duplicate key wins, pinning the
mode regardless of the inherited environment (rationale at `main.go:74-79`), and
`cmd/converge-cli/main_test.go:154-176` tests exactly that with
`t.Setenv("CONVERGE_MODE", "hosted")`. The documented residual is now a documentation
defect: it tells operators to work around a hazard that no longer exists, and it
mis-describes the binary's behaviour. One-paragraph fix.

**I-2. `internal/auth/store.go:13` asserts a repository-wide exclusivity that is false.**
"Store is the only file in this repository that contains SQL." It is not:
`internal/db/db.go:85` creates `schema_migrations`, `:122` selects from it, `:181` inserts
into it, and `internal/db/migrations/0001_init.sql` is pure DDL. The constraint's
*substance* is intact — all value-bearing SQL is parameterised, and all *domain* SQL is in
`store.go` — but the sentence as written is disproved by a one-line grep, which makes it a
trap for the next reader auditing the same invariant. Narrow the claim to "the only file
containing domain SQL; schema and migration bookkeeping live in `internal/db`."

**I-3. `providerResolverTTL` bounds token *use*, not token *residency* — contradicting its
own doc comment.**
`internal/auth/resolver.go:15-16` says the constant "bounds how long a user's decrypted
tokens sit in memory." It does not. `Resolve` checks the TTL before *returning* a cached
entry (`resolver.go:75-77`) and overwrites the entry on a miss (`:82-84`), but nothing
evicts on elapse: the only deletion is `Invalidate` (`resolver.go:90-94`), called on
provider create/update/delete (`providers.go:81, 155, 178`) and account deletion
(`service.go:327`). A user who authenticates once and never returns leaves a
`*provider.Registry` holding plaintext `config.Secret` tokens resident in the `cache` map
for the remaining lifetime of the process. Two consequences: an unbounded map keyed by
user id (memory growth proportional to every user ever seen since boot), and a plaintext
token residency window of "forever" rather than 15 minutes — which is the property the
comment advertises and which a memory-disclosure or core-dump threat model would rely on.
Either add TTL eviction (a sweep pass over `cache`, or evict-on-read-miss) or correct the
comment to say the TTL bounds staleness, not residency.

**I-4. `internal/auth` has no `builder.go` (DOM-01 FAIL).**
`ai-guidance.md:170` and `scaffolding-checklist.md:139` require "every domain with a model"
to have a fluent builder with `Build()` validation. `internal/auth/model.go` declares two
models (`User:94`, `UserProvider:159`) and the package has no `builder.go`; construction is
via `NewUser` (`model.go:104`) and `NewUserProvider` (`model.go:185`). The consequence shows
up at `providers.go:105-151`, where `UpdateProvider` assigns six model fields directly
because there is no `row.Builder()` to go through — a direct violation of
`patterns-functional.md:13` ("all mutations occur via builders returning new instances").
`internal/session/builder.go:18` shows the pattern the codebase already follows elsewhere,
so this is an inconsistency within the project, not only against the skill.

Mitigating: the validating constructors do enforce invariants (`model.go:105-107` rejects
a bad username and empty id/hash; `model.go:186-189` rejects a bad slug and a missing
sealed token), slices are defensively copied on both ingress and egress
(`model.go:199-201`, `:216-224`), and `UpdateProvider` mutates a *value copy* so no caller
observes a partially-updated model. No correctness or security impact — this is a
structural conformance FAIL.

### Minor

**M-1. 422 responses echo internal error text.**
`internal/api/settings_providers.go:184` passes `err.Error()` straight into the
client-facing detail, so a malformed slug returns `"auth: a slug is 1 to 32 characters of
lowercase letters, digits, and hyphens, starting with a letter or digit: auth: invalid
input"` — package prefix, duplicated prefix, and the sentinel's text. The messages are
deliberately value-free (`auth/errors.go:36-38`), so nothing sensitive escapes; it is
contract cosmetics. Contrast the adjacent paths, which return curated messages
(`auth/errors.go:52`, `model.go:52-54`).

**M-2. A store fault during login is reported as `INVALID_CREDENTIALS` (401).**
`internal/auth/service.go:165-184` branches on `lookupErr != nil` without distinguishing
`ErrNotFound` from a genuine database failure, so a `UserByFold` error of any kind yields
401 plus a consumed throttle slot rather than 500. Fails closed and preserves the
timing-equalisation property, but mislabels a server fault and lets a transient DB problem
burn a user toward lockout.

**M-3. `Register` never clears the throttle on success.**
`Login` calls `Throttle.Succeed` after a successful verify (`service.go:185`), but
`Register` (`service.go:126-161`) does not, so IP failures accrued before a successful
registration stay on the counter. Arguably intentional — registration is throttled per IP
precisely to limit flooding — but it is an asymmetry with no comment explaining it.

**M-4. Commit `5fcb4cf`'s message still carries the overstated leak claim.**
Its body reads "no hosted token was in that list and one reaching git's stderr was logged
in clear." `.superpowers/sdd/plan/residuals.md` records that a later review established
there is no currently reachable production path by which a *raw* token lands on git stderr,
and `docs/tasks/task-005-hosted-multi-user-mode/execution-notes-6.md:33` carries the
corrected "defence in depth, not closure of a live leak" framing. I found no *code comment
or doc* still asserting the stronger claim — `internal/provider/github/client.go:189-195`
and `gitlab/client.go:239-242` are both accurately hedged. Commit messages cannot be
corrected without a history rewrite; the PR description should carry the corrected framing
so the merge commit is the accurate record.

**M-5. DOM-19 WARN.** Seven new test files use neither a `[]struct` table nor `t.Run`
subtests (enumerated in the DOM-19 row above). `testing-guide.md:39` states a preference,
so this is not a FAIL; noted because the branch adds ~2,900 lines of test code in that
style and it will set the local convention.

## Residual Triage

**R-1 — Task 26, the credential-injection path is unexercised by any test. → Still fine.
Non-blocking.**
The ruling's core reasoning verifies: `gitx.Run` logs category, repo, session, exit code,
duration, outcome, and `Redact`-ed stderr, and `Spec.Args`/`Spec.Env` appear in no log call
anywhere in the function (`internal/gitx/exec.go:172-196`). The gap is also narrower than
the residual describes, because guard tests now exist at the declaration layer:
`internal/provider/github/authorize_git_spec_secret_test.go:15-61` asserts the raw token is
absent from `spec.Env` *and* that both the token and the Basic blob are declared in
`spec.Secrets`; `internal/provider/gitlab/authorize_git_spec_secret_test.go:19-62` does the
same for GitLab; and `internal/provider/github/authorize_git_secret_test.go:33-113` drives
real git stderr end to end for both the raw token and the bare blob. What remains
unexercised is only the *composition* — a real review build through a real provider that
actually populates `GIT_CONFIG_*`. The named cost ("if future code logs `Spec.Args` or
`Spec.Env`, no test catches it") is real but is a hypothetical-future-regression guard, not
a present defect. I agree with the parking.

**R-2 — `gitx.ExitError.Result.Stderr` is a raw exported field. → Still fine. Ticket, not
this branch.**
`(*ExitError).Error()` emits only category and exit code (`internal/gitx/spec.go:58-60`),
and a repo-wide grep for `.Stderr` finds no production caller outside `internal/gitx`.
Reaching a log requires someone to write `slog.Any("err", ee)` *and* for `Result` to be
reached by the JSON handler's reflection — two future steps, neither present. A
`LogValue()` on `Result` is cheap and worth a follow-up ticket; it is not merge-blocking.

**R-3 — `spec_secrets_test.go` GitLab-filename asymmetry. → Still fine. Naming only.**
`internal/provider/gitlab/authorize_git_spec_secret_test.go:12-62` asserts both the raw
token and the Basic blob are declared, and the redaction mechanism they feed is shared
(`internal/gitx/exec.go:113-121`, `:190`). The only thing GitHub has that GitLab does not
is the end-to-end real-git-stderr exercise, and that tests `gitx`, not the provider. No
coverage gap.

**R-7 — `CLAUDE.md:60` says `internal/auth` "may import `db`". → Agree with the ruling.
Non-blocking; tightening optional.**
Confirmed independently: `go list -deps ./internal/auth` returns `config`, `identity`,
`gitx`, `provider`, `provider/github`, `provider/gitlab` — no `internal/db`. `NewStore`
takes a bare `*sql.DB` (`internal/auth/store.go:32`), so the coupling is to `database/sql`,
not to the package. The sentence grants a permission that the code declines to exercise,
which is not a crossed boundary, and it mirrors the doc comment at
`internal/auth/errors.go:5-10` (which has the same wording). Tightening both to describe
what the code actually does would be an improvement; leaving them is not a defect.

**R-8 — `cmd/converge-cli` honouring `CONVERGE_MODE`. → I differ from the recorded ruling.
Doc fix needed (see I-1).**
The residual asks me to "confirm the documented behaviour matches the code." It does not.
The residual and `docs/hosted-mode.md:106-110` both describe a CLI that would open the
hosted database if `CONVERGE_MODE=hosted` were inherited; `cmd/converge-cli/main.go:80`
pins `CONVERGE_MODE=standalone` and `main_test.go:154-176` tests it. So the *code* residual
was closed and only the *documentation* was left behind. My verdict: the code is correct
and better than documented; the doc is wrong and should be corrected before merge, since
it actively misinstructs operators.

**R-4, R-5, R-6** are frontend / test-helper / coverage-breadth items outside this audit's
scope. R-4 (native `<select>` vs. Radix `Select`) is explicitly the frontend reviewer's
ruling to make.

## The `internal/auth` Flake — Explicit Call

**The evidence retires it.** I could not identify a shared-state mechanism that would
produce a load-dependent failure in `internal/auth`:

- **No shared clock.** Time is injected per construction (`ServiceDeps.Now`,
  `internal/auth/service.go:65`; `NewThrottle(store, now)`, `throttle.go:47`), so tests
  advance their own clock rather than racing a global.
- **No shared temp dir or database.** Each store test opens its own handle; `db.Open`
  creates the parent directory per path (`internal/db/db.go:56-60`).
- **No unsynchronised counter.** The one read-modify-write is wrapped in a transaction
  (`Store.UpdateAttempt`, `store.go:382-417`), and the pool is pinned to a single
  connection (`db.go:70-71`), which serialises it.
- **Only one piece of package-level mutable state**, `dummyHash` (`password.go:183`), and it
  is written exactly once by `mustDummyHash()` at init and read-only thereafter
  (`service.go:170`).

The one coupling I *can* name, and the most plausible mechanism if it ever resurfaces:
every `internal/auth` test that hashes or verifies pays real Argon2id at 64 MiB
(`password.go:29`), and `hashConcurrency` is 4 (`service.go:29`), so the package can hold
~320 MiB transient while `go test ./...` runs other packages in parallel. Under genuine
memory pressure that surfaces as a timeout or an allocation failure — **not** as a wrong
assertion. So if it reappears, the first datum to capture is whether the failure is a
timeout/OOM or an assertion: the former points at the Argon2 memory budget under parallel
package load and is a harness-capacity question; only the latter would indicate a real
logic race. Given ~20 consecutive clean runs including a dedicated
`go test -race -count=3 ./internal/auth/` hunt and two full CI gates, I do not consider
this a merge risk.

## Summary

### Blocking (must fix)

None. No security, isolation, or correctness defect was found.

### Should fix before merge (cheap, and both are correctness-of-record issues)

- **I-1** `docs/hosted-mode.md:106-110` — delete or rewrite the stale `converge-cli`
  `CONVERGE_MODE` warning; the code pins standalone at `cmd/converge-cli/main.go:80`.
- **I-2** `internal/auth/store.go:13` — narrow "the only file in this repository that
  contains SQL" to exclude `internal/db`'s schema bookkeeping.

### Should fix (guideline conformance / bounded risk)

- **I-3** `internal/auth/resolver.go:15-16` — either evict cache entries on TTL elapse or
  correct the comment; as written the plaintext-token residency claim is wrong.
- **I-4 / DOM-01 + DOM-IMM** `internal/auth` — add `builder.go` with `NewBuilder()`,
  fluent setters, `Build()` validation, and a `Builder()` method on each model, then route
  `UpdateProvider` (`providers.go:105-151`) through it.

### Non-blocking

- **M-1** `settings_providers.go:184` returns raw `err.Error()` as the 422 detail.
- **M-2** `service.go:165-184` maps a store fault to 401 instead of 500.
- **M-3** `Register` (`service.go:126-161`) does not call `Throttle.Succeed`.
- **M-4** commit `5fcb4cf`'s body retains the overstated leak framing; carry the corrected
  framing in the PR description.
- **M-5 / DOM-19** seven new test files are one-func-per-case rather than table-driven.
- **R-2** a `LogValue()` on `gitx.Result` is worth a follow-up ticket.

### Verdict

**Checklist status: NEEDS-WORK** — DOM-01 and DOM-IMM fail for `internal/auth`, and per the
audit contract a single FAIL prevents an overall PASS. There is no curve.

**Merge recommendation: approve once I-1 and I-2 are fixed.** Both are one-paragraph
documentation edits to statements that are *disprovably* false, on a branch whose entire
safety argument rests on documented invariants being trustworthy. The DOM-01 failure is
structural conformance with no correctness or security consequence and can land as a
follow-up. I-3 is the only finding with any runtime weight, and its impact is memory
residency rather than disclosure.

All five CI gates pass. Every binding security constraint in the plan — SQL
parameterisation, shell-free git with env-only credential injection, exact Argon2id and
AES-256-GCM parameters with AAD row-binding, cookie shape and hash-only persistence,
secret redaction, dependency direction, `CGO_ENABLED=0` with the pure-Go driver, the ten
error-code/status pairs verbatim, and multi-tenant isolation across sessions, mirrors, and
provider configs — verified with file:line evidence above. I found no path by which one
user reads, mutates, or deletes another's data, no path by which a secret reaches a log,
and no 403-vs-404 existence oracle.

---

# Reviewer report — frontend FE-* checklist

# Frontend Audit — task-005-hosted-multi-user-mode

- **Audit Scope:** `.superpowers/sdd/plan/final-frontend.diff` (41 files, +3591/-28), branch range `7ae00b9..f916444`
- **Guidelines Source:** `frontend-dev-guidelines` skill, with the three agreed CLAUDE.md deviations (Vitest, thin fetch wrapper, plain service objects) excluded from findings
- **Date:** 2026-09-11
- **Build:** PASS (per `.superpowers/sdd/plan/task-28-gate.log`; not re-run in this audit per instructions)
- **Tests:** 337 passed, 0 failed (42 test files) — `make test` exit 0; `make build`/`make docker-build` exit 0
- **Overall:** NEEDS-WORK

## Build & Test Results (from task-28-gate.log, not re-executed)

```
EXIT_CODE(make lint)=0
EXIT_CODE(make test)=0              Test Files 42 passed, Tests 337 passed
EXIT_CODE(make test-integration)=0
EXIT_CODE(make build)=0
EXIT_CODE(make docker-build)=0
```
No frontend lint/format/test/build failures in the captured log.

## File Inventory

- Page: `apps/frontend/src/pages/LoginPage.tsx`, `RegisterPage.tsx`, `ProviderSettingsPage.tsx`, `AccountSettingsPage.tsx`
- Component (auth): `components/auth/AuthProvider.tsx`, `ModeGate.tsx`, `RequireAuth.tsx`
- Component (features/settings): `components/features/settings/UserProviderForm.tsx`, `UserProviderList.tsx`
- Component (layout): `components/layout/AccountMenu.tsx`, `AppShell.tsx` (modified)
- Component (ui): `components/ui/dropdown-menu.tsx` (modified — added Item/Label/Separator/Group)
- Hook: `lib/hooks/api/useAuth.ts`, `lib/hooks/api/useUserProviders.ts`
- Service: `services/api/auth.ts`, `services/api/userProviders.ts`, `services/api/index.ts` (modified)
- Schema: `lib/schemas/auth.ts`, `lib/schemas/userProvider.ts`
- Type: `types/models/auth.ts`
- Other: `App.tsx` (modified), `routes.tsx` (modified), `lib/api/client.ts` (modified), `lib/api/formErrors.ts` (new), plus a large matching set of `__tests__` files for every item above

## Anti-Pattern Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-01 | No `any` type | PASS | Grepped all 22 in-scope source files for `: any`/`as any` — zero matches. |
| FE-02 | No manual class concatenation | PASS | Zero `className={"` + concatenation matches across in-scope files; all conditional classes use template literals inside `cn()`-free static strings or plain string literals (no concatenation needed). |
| FE-03 | No direct API client calls in components | FAIL (minor) | `apps/frontend/src/components/auth/AuthProvider.tsx:5` imports `setUnauthorizedHandler` directly from `@/lib/api/client`. This is not a data-fetch bypass — it registers a 401 callback, the documented seam per the doc comment at `lib/api/client.ts:1936-1940` explaining the module stays free of React imports — but it is a literal match against the mechanical check and the only lib/api/client import in a component file. |
| FE-04 | No inline Zod schemas in components | PASS | No `z.object(`/`z.string(` in any `components/**` or `pages/*.tsx` file in scope; all schemas live in `lib/schemas/auth.ts` and `lib/schemas/userProvider.ts`. |
| FE-05 | No spinners for content loading | PASS | All `animate-spin` occurrences are on submit buttons: `UserProviderForm.tsx:152,275`, `UserProviderList.tsx:133` (delete-confirm button), `AccountSettingsPage.tsx:149,237`, `LoginPage.tsx:78`, `RegisterPage.tsx:96`. Content loading uses `Skeleton` (`UserProviderList.tsx:1039-1044`) or `aria-busy` placeholders (`ModeGate.tsx:418`, `RequireAuth.tsx:450`). |
| FE-06 | No hardcoded colors | PASS | Zero matches for `bg-white|black|gray-N|red-N|green-N|blue-N` across in-scope files; all use semantic tokens (`bg-background`, `text-destructive`, `border-border`, etc.). |
| FE-07 | No state mutation | PASS | Zero `.push(`/`.splice(`/`.sort(` in in-scope files; `UserProviderList.tsx` uses `useState` replacement, not array mutation. |
| FE-08 | No default exports for components | PASS | Zero `export default function` in in-scope files; every component/page/hook/service is a named export (e.g. `export function LoginPage()` at `pages/LoginPage.tsx:14`). |
| FE-09 | Error handling with `createErrorFromUnknown` | PASS (codebase-wide equivalent) | `createErrorFromUnknown` does not exist anywhere in this codebase (confirmed via repo-wide grep — zero hits). The actual, consistently-used project pattern is `ApiError`/`isApiError`/`messageFor` (`lib/api/errors.ts:2,16,21`) surfaced via `applyServerError` (`lib/api/formErrors.ts:2040-2053`) into `setError`/`toast`. Every `try { await mutateAsync(...) } catch` block in scope (`UserProviderForm.tsx:763-769,894-899`, `LoginPage.tsx:984-986`, `RegisterPage.tsx:3143-3149`, `AccountSettingsPage.tsx:2782-2790,2880-2882`) routes through this mechanism — none swallow an error silently. This is a pre-existing, whole-codebase deviation from the literal guideline text, not something task-005 introduced, and is not re-litigated as a new finding. |

## Architecture Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-10 | JSON:API model shape | FAIL (minor) | `types/models/auth.ts:4-13,18-26` defines `AuthMode`, `CurrentUser`, and `UserProvider` as flat interfaces (`{ id, username, ... }`), not as `Resource<"type", Attributes>` the way every other model in the codebase does it — compare `types/models/provider.ts:9` (`export type Provider = Resource<"providers", ProviderAttributes>`) and `types/models/repository.ts:9`. The wire-level JSON:API shape is still respected at the service boundary (`services/api/auth.ts:3996-4003`, `services/api/userProviders.ts:4087-4091` define `Resource<...>` wire types and unwrap/flatten them into the domain model), so no JSON:API violation reaches the wire, but the domain-model file itself diverges from the established in-repo pattern. |
| FE-11 | Service extends `BaseService` (when applicable) | PASS (per agreed deviation) | `services/api/auth.ts` and `services/api/userProviders.ts` use the plain-object pattern (`export const authService = {...}`), consistent with the CLAUDE.md-documented deviation and with the pre-existing `providersService`/`changesService` in this codebase. Not a finding. |
| FE-12 | Query key factory uses `as const` | PASS | `lib/hooks/api/useAuth.ts:3-6` (`authKeys = { mode: [...] as const, me: [...] as const }`); `lib/hooks/api/useUserProviders.ts:6-8` (`userProviderKeys = { all: ["userProviders"] as const }`). |
| FE-13 | Forms use `react-hook-form` + `zodResolver` | PASS | `LoginPage.tsx:975-978`, `RegisterPage.tsx:3133-3136`, `AccountSettingsPage.tsx:2769-2772,2866-2869`, `UserProviderForm.tsx:750-753,869-872` all call `useForm({ resolver: zodResolver(...) })`. |
| FE-14 | Schema in `lib/schemas/` with inferred type | PASS (one minor exception) | `lib/schemas/auth.ts` and `lib/schemas/userProvider.ts` each pair a `z.object(...)` with `export type X = z.infer<typeof x>` (e.g. `auth.ts:2602-2603,2611`). Exception: `DeleteAccountFormData` (`auth.ts:2636`) is hand-declared as a literal interface rather than `z.infer`, because `deleteAccountSchema` is a factory parameterized by the live username (`auth.ts:2630-2635`) — a reasonable, narrow exception, not flagged as blocking. |

## Styling Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-15 | Interactive elements show `cursor-pointer` | PASS | All custom clickable surfaces in scope are `<Button>` (cursor-pointer baked into its CVA definition) or `DropdownMenuItem`, which itself gained `cursor-pointer` in this diff (`components/ui/dropdown-menu.tsx:1623`, `"relative flex cursor-pointer items-center..."`). No raw `onClick`-bearing `<div>` without `cursor-pointer` was found in any in-scope file. |

## Testing Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-16 | Tests exist for changed components | PASS | Every new/modified component, hook, page, service, and schema has a matching `__tests__` file: `AuthProvider.test.tsx`, `ModeGate.test.tsx`, `RequireAuth.test.tsx`, `UserProviderForm.test.tsx`, `AccountMenu.test.tsx`, `AppShell.test.tsx` (extended), `useAuth.test.tsx`, `useUserProviders.test.tsx`, `auth.test.ts` (schemas), `LoginPage.test.tsx`, `RegisterPage.test.tsx`, `ProviderSettingsPage.test.tsx`, `AccountSettingsPage.test.tsx`, plus `App.test.tsx`/`routes.test.tsx` extended for mode-gating. `UserProviderList.tsx` has no dedicated test file, but its behavior (empty state, row actions, delete confirm/error) is fully exercised indirectly through `ProviderSettingsPage.test.tsx`. |
| FE-17 | Mocks updated when services changed | PASS (N/A pattern) | This codebase uses MSW (`@/test/server`) rather than a `__mocks__/` directory (confirmed: no `__mocks__` directory exists anywhere in `apps/frontend/src`). Every new endpoint (`/api/auth/*`, `/api/settings/providers*`) has matching MSW handlers registered per-test via `server.use(...)`, e.g. `UserProviderForm.test.tsx:1195-1211`, `AccountSettingsPage.test.tsx:3329-3334`. |

## Residual Rulings (not re-reported as new findings)

### Residual item 4 — native `<select>` vs. Radix `Select`

**Confirmed and still open — recommend fixing before merge, not shipping as-is.**

- `components/features/settings/UserProviderForm.tsx:802-809` (create form) and `:931-938` (edit form) both render a native `<select>` for the provider `kind` field (`github`/`gitlab`).
- The rest of the codebase uses the Radix-based `Select`/`SelectTrigger`/`SelectContent`/`SelectItem` from `components/ui/select.tsx` for an equivalent "pick a provider kind" control: `components/features/providers/ProviderPicker.tsx:1-7,31-41` renders exactly this kind of two/few-option dropdown, including for the same `Provider.kind` concept (`provider.attributes.kind`), via Radix `Select`.
- The `frontend-dev-guidelines` forms pattern (`patterns-forms-validation.md:145-170`) shows the canonical Select-in-a-form pattern with `FormField`/`field.onChange`, which is exactly the shape needed here (`register("kind")` would become `onValueChange={(v) => setValue("kind", v)}` or a `Controller`).
- Divergence is real, not cosmetic: the native `<select>` does not pick up the app's Radix-driven focus ring, open/close animation, or popover styling (`SelectContent`'s `data-[state=open]:animate-in` etc. in `components/ui/select.tsx`), and native `<select>` popups are rendered by the OS, not themed by the app's dark-mode CSS variables the way `SelectContent`'s `bg-popover`/`text-popover-foreground` are. It is a visible, users-will-notice inconsistency between this settings page and the rest of the app, not just an internals difference.
- Swap is contained: the field has exactly 2 static options, appears in exactly 2 places (create/edit forms, both in the same file), and the pattern to copy already exists verbatim in `ProviderPicker.tsx`.
- **Recommendation: fix before merge.** This is the one residual with real design weight, and the fix is small, low-risk, and has a working reference implementation already in-tree.

### Residual item 5 — `renderPageWithClient` duplicates `render.tsx`

**Confirmed and still open — recommend consolidating, non-blocking for merge.**

- `apps/frontend/src/pages/__tests__/AccountSettingsPage.test.tsx:3277-3295` defines `renderPageWithClient()`, a hand-rolled `render()` call wrapping `QueryClientProvider` + `ThemeProvider` + `MemoryRouter`, built solely to expose the `QueryClient` for cache assertions.
- `apps/frontend/src/test/render.tsx` already solves exactly this problem for hooks via `queryWrapper({ gcTime })` (`render.tsx:13-30`), which returns a wrapper with `.client` attached, but `renderWithProviders` (`render.tsx:33-41`, the component-render helper actually used by page tests) does not expose its internal client, which is why `AccountSettingsPage.test.tsx` had to fork its own copy instead of extending the existing helper.
- This is containable: `renderWithProviders` could accept an optional `client` (or return `{ ...utils, client }` the way `queryWrapper` already does for hooks), eliminating the duplicate markup (`ThemeProvider`+`MemoryRouter`+`QueryClientProvider`) in `AccountSettingsPage.test.tsx`.
- **Recommendation: consolidate, but non-blocking.** Low risk, no behavior change, but this is accumulating test-helper debt — a second page test that needs cache inspection will either re-duplicate `renderPageWithClient` again or diverge from it. Worth a fast-follow fix, not a merge blocker.

## Summary

### Blocking (must fix)
- None. All FAIL items below are minor/non-blocking; build and tests are green.

### Non-Blocking (should fix)
- **FE-10**: `types/models/auth.ts` models (`CurrentUser`, `UserProvider`, `AuthMode`) are flat interfaces rather than `Resource<"type", Attributes>`, diverging from every other model in `types/models/`. No wire-level violation (service layer correctly wraps/unwraps JSON:API), but inconsistent with repo convention.
- **FE-03**: `AuthProvider.tsx:5` imports directly from `lib/api/client` rather than a service. Architecturally justified (401-handler registration, not data fetching) per its own doc comment, but it is the literal anti-pattern match.
- **Residual item 4 (native `<select>`)**: recommend fixing before merge — contained swap, visible/behavioral inconsistency, reference implementation already in-tree (`ProviderPicker.tsx`).
- **Residual item 5 (`renderPageWithClient` duplication)**: recommend consolidating into `render.tsx`'s `renderWithProviders`/`queryWrapper`, non-blocking.

### Overall merge verdict
**NEEDS-WORK but mergeable with a fast-follow.** No blocking FE-* failures, build/tests are green end-to-end (backend + frontend, lint, test, test-integration, build, docker-build all exit 0 per the captured gate log). The only items with real weight are the residuals: the native `<select>` is the one genuine UI-consistency defect and should be fixed before merge given how small the fix is; the test-helper duplication and the two minor FE-03/FE-10 findings are acceptable to ship and track as fast-follows.

---

# Reviewer report — whole-branch merge review

# Final whole-branch review — task-005 hosted multi-user mode

Reviewer: broad merge review (correctness, security, architecture, maintainability, coherence).
Range `7ae00b9..f916444`, 50 commits, 140 files, +24,703/−271.
Scope note: per the brief I ran **no** build/test gate. Evidence for the gates is the captured
`.superpowers/sdd/plan/task-28-gate.log` (all five commands, real output, `EXIT_CODE=0` for
`make lint`, `make test`, `make test-integration`, `make build`, a post-frontend-build
`go test -race ./...`, and a genuine `make docker-build` image export). Read-only commands I ran:
`git log/diff/show`, `grep`, `sed`, `go list -deps ./internal/auth`, `go list -deps ./internal/identity`,
`go list -deps ./internal/db`.

## Verdict

**Merge, after the four Important findings below.** None of them is a correctness or security
defect in the hosted path, none is a standalone regression, and I found no cross-tenant read,
write, or delete path. Two of the four are documentation/comment statements that are now false,
one is a duplicated form body, one is a throttle-reset semantic that contradicts its own stated
requirement. All four are small, local edits.

## What this branch gets right

These are verified, not courtesies.

- **Standalone equivalence is evidenced, not asserted.** `config.Load` reaches `loadHosted` only
  under `CONVERGE_MODE=hosted` (`internal/config/config.go:165-169`), so `DatabasePath` and
  `SecretKey` stay zero; `app.New` gates every hosted construction on the same condition
  (`internal/app/app.go:310-345`), and `db.Open` is unreachable otherwise. The test that proves
  it asserts the right things — `App.DB == nil`, `App.Auth == nil`, *and* `os.Stat` on both the
  configured path and the default `/data/converge.db`
  (`internal/app/app_test.go:378-406`). On the HTTP side the auth/settings routes are not
  registered at all in standalone (`internal/api/router.go:130-145`) so the existing `/api/`
  catch-all produces the 404s, with no "if standalone" branch in any handler;
  `TestStandaloneRouterIsUnchanged` (`internal/api/isolation_test.go:377-407`) walks all six
  routes and asserts 404. The two middlewares are constructed only in hosted mode
  (`router.go:163-170`), so standalone's request path genuinely gains zero comparisons.
  The one additive change in standalone is `GET /api/auth/mode`, registered in both modes
  (`router.go:113`) because the SPA needs it before it knows anything — additive, documented,
  and correct.
- **The zero-value `identity.Scope` question is answered everywhere it matters.** The field is
  unexported so a `Scope` cannot be forged outside the package (`internal/identity/scope.go:9-16`).
  Every hosted entry point is wrapped by `authenticate` (`router.go:168`), so `scopeFrom`'s
  standalone fallback (`internal/api/authctx.go:30-35`) is unreachable for a scoped deployment.
  Where a zero scope would be dangerous, it is refused rather than defaulted:
  `auth.ProviderResolver.Resolve` errors on an unscoped identity in hosted mode
  (`internal/auth/resolver.go:64-70`), `session.Store.Purge` refuses an empty user id
  (`internal/session/store.go:286-288`), `mirror.Cache.PurgeNamespace` refuses the root namespace
  (`internal/mirror/cache.go:105-117`), and `Sealer.Seal` refuses empty ids
  (`internal/auth/crypt.go:64-66`). `Service.PurgeUser` crosses two of those guards at once
  (`internal/review/service.go:643-655`).
- **404-vs-403 is decided by the store, not by handlers.** `Store.Get`/`Store.List` filter on
  `scope.Matches(owner)` (`internal/session/store.go:186-222`), which flows into the existing
  `ErrNotFound → 404` mapping. Every SQL read for a provider configuration carries `user_id` in
  the WHERE clause (`internal/auth/store.go:266-331`), so a foreign id and an unknown id are
  indistinguishable. The two deliberate handler-level exceptions are both recorded in the plan
  and both check out: `deleteReview` (`internal/api/reviews.go:160-190`) keeps the pre-hosted
  unconditional 204 in standalone and answers 404 for unknown *and* foreign ids in hosted;
  `sessionFor` consults `Corrupted` only when unscoped (`reviews.go:121-135`), which is right —
  a 500 on a corrupt record would confirm the id exists.
- **Token-at-rest handling is sound.** AES-256-GCM with `user_id\0provider_id` as AAD
  (`internal/auth/crypt.go:49-73`) means a ciphertext moved between rows or users fails to open.
  `UserProvider` has no plaintext-token field at all (`internal/auth/model.go:165-181`), the wire
  shape has none either (`internal/api/settings_providers.go:19-28`), and `token_last4` is
  captured at write time so rendering a mask never decrypts. Argon2id reads m/t/p back from the
  stored PHC string with ceilings that fail closed on a tampered row
  (`internal/auth/password.go:36-46, 138-143`) — the OOM/hang vector is closed and tested with a
  deadline rather than by eyeballing.
- **Architecture holds mechanically and substantively.** `go list -deps` confirms production
  `internal/auth` imports only `config`, `identity`, `provider`, `provider/github`,
  `provider/gitlab` (and `gitx` transitively) — no `review`, no `session`, no `db`;
  `internal/identity` and `internal/db` are stdlib-only leaves. The account-deletion cascade
  crosses the boundary through `auth.Purger`/`auth.ProviderUsage` wired in `internal/app`
  (`internal/auth/service.go:31-47`, `internal/app/app.go:354-358`), which is the correct
  direction. More importantly it does not *feel* bolted on: the `provider.Resolver` seam
  (`internal/provider/resolver.go`) is the one real architectural change, and it is the right
  one — "the set of providers is a function of the caller" is exactly what hosted mode means, and
  standalone gets a three-line static wrapper. `internal/auth` is large (≈2,100 production lines
  across 9 files) but it is not doing too much: each file is a distinct concern and the SQL is
  confined to one.
- **`mirror` namespacing is explicit, not implicit.** `Namespace` is a required argument on
  `Path`/`Ensure`/`FetchSHA` (`internal/mirror/cache.go:77-180`), so every call site had to be
  visited by the compiler, and a user id is re-validated against `^[0-9a-f]{16}$` before it
  becomes a directory name (`cache.go:18-22`) rather than trusted because it came from a session.
  `TestTwoUsersReviewTheSameRepositoryInSeparateMirrors`
  (`internal/review/hosted_integration_test.go:141-212`) asserts both mirrors exist, that the
  paths differ, that nothing landed under the bare cache root, that A's list contains only A's
  session, and that A cannot `Get` B's — real evidence, not a smoke test.
- **Secret redaction has a real mechanism and the honest limits are written down.** The
  per-invocation `Spec.Secrets` unioned with the runner-wide list (`internal/gitx/exec.go:109-120`)
  is the only channel a per-user token has, because `Options.Secrets` is fixed at construction;
  both providers declare the raw token *and* the base64 Basic blob derived from the shared
  `gitx.BasicAuthBlob` (`internal/provider/github/client.go:189-196`,
  `internal/provider/gitlab/client.go:238-243`). I tried to find a path through it: the only
  production consumer of unredacted stderr would be `ExitError.Result.Stderr`, and `grep` shows
  **no production caller reads it** (the only `.Stderr` references outside `gitx` are
  `os.Stderr` writers and test helpers), while `exec.go:159-190` logs only category, repo,
  session, exit code, duration, and redacted stderr — never `Args` or `Env`.

## Critical

None.

## Important

### I1. `docs/hosted-mode.md:102-110` describes `converge-cli` behaviour that the code no longer has

The closing section states: "`CONVERGE_MODE=hosted` present in the CLI's own environment would
still cause it to attempt to open the database — operators should not set `CONVERGE_MODE=hosted`
in a shell where `converge-cli` runs."

That was true when the doc was written and stopped being true in commit `fc657d4`.
`cmd/converge-cli/main.go:74-79` now calls
`newApp(ctx, append(os.Environ(), "CONVERGE_MODE=standalone"))`, and `config.Load` builds its
map by iterating the slice in order (`internal/config/config.go:123-129`) so the appended
duplicate wins. The CLI is pinned to standalone and cannot open the database.

This matters more than a stale sentence usually would, because the documented hosted deployment
puts `CONVERGE_MODE=hosted` in the container's environment (`docker-compose.yml:40-42`) — i.e.
the doc tells an operator to avoid the exact configuration the project ships, for a reason that
no longer exists, and simultaneously misdescribes what the binary does.

This is recorded residual #8, whose ruling was "confirm the documented behaviour matches the
code". It does not. Fix the doc (the behaviour is correct).

### I2. `apps/frontend/src/components/features/settings/UserProviderForm.tsx:73-158` and `:196-281` are the same form body twice

`CreateUserProviderForm` and `EditUserProviderForm` are two ~110-line components whose JSX is
identical except for four things: the slug field (editable input vs. read-only text), the token
placeholder, the submit label, and one extra error-code mapping. A mechanical diff of the two
return bodies shows roughly 75 of 86 lines matching verbatim, including all of the display-name,
kind, base-URL, and token field markup and their `aria-invalid`/error-paragraph wiring.

The failure mode is the ordinary one: the next field, validation tweak, or accessibility fix
lands in one form and not the other, and nothing in the test suite notices because each form has
its own test. Extract the shared fields into one component that takes the register function and
errors, keeping the four genuine differences as props.

### I3. `apps/backend/internal/auth/store.go:107-109` claims a cleanup that does not happen, and the lockout outlives the account

`DeleteUser`'s doc comment says "Login sessions, provider configs, and the user's own attempt row
cascade via ON DELETE CASCADE / explicit key match (FR-2.7)". The first two do cascade
(`internal/db/migrations/0001_init.sql:21, 31`). The third does not: `login_attempts` has no
foreign key at all (`0001_init.sql:47-54`) — it is keyed `(scope, key)` where `key` is a folded
username or an IP — and `DeleteUser` executes only `DELETE FROM users`
(`store.go:110-123`). There is no "explicit key match" anywhere; `grep` shows the only other
writers are `ClearAttempt` (on successful login) and `DeleteElapsedAttempts` (the sweeper).

Consequence beyond the wrong comment: a user who deletes their account while holding a
non-zero failure counter leaves that row behind, and because the key is the *folded username*
rather than a user id, a subsequent registration of the same username inherits it — including an
in-force `locked_until`. The window is bounded by the sweeper, but the sweeper's own condition
(see I4) is not obviously tight enough to make that reasoning safe by construction.

Either delete the user's `login_attempts` row in `DeleteUser` (one `ExecContext`, same
transaction-free style as the rest), or correct the comment and accept the inheritance
explicitly. I would do the former.

### I4. The username failure counter is windowed by the sweeper, contradicting `throttle.go:83-84` and FR-7.2

`internal/auth/throttle.go:82-88` documents, and the code implements, a deliberate asymmetry: the
IP counter is windowed at 15 minutes, "the username counter is consecutive-failure based and
resets only on success (FR-7.2), so it has no window."

The sweeper undoes that. `Throttle.Sweep` calls
`DeleteElapsedAttempts(now - ipWindow)` (`throttle.go:123-125`) and the statement is
`DELETE FROM login_attempts WHERE locked_until <= ? AND window_start <= ?`
(`store.go:429-432`) — with no `scope` predicate. For a user-scope row that has not yet reached
the threshold, `locked_until` is the zero time (persisted as a large negative unix value by
`unix(time.Time{})`, `store.go:34`), so the first clause is trivially true, and `window_start` is
the timestamp of the first failure. Any user counter whose first failure is more than 15 minutes
old is therefore deleted on the next sweep — i.e. the username counter *does* reset without a
success, on a schedule of `CLEANUP_INTERVAL_MINUTES` (default 30).

Practical effect: an attacker pacing guesses so that no counter survives a sweep never reaches
the five-failure username lockout. The residual protection is the per-IP counter (20 per 15
minutes), which a distributed attacker does not pay. The absolute rate this permits is low, so
this is not a break — but it is a documented security control behaving differently from its
documentation and from the FR it cites, which is exactly the class of thing that gets relied on
later.

Two acceptable fixes: add `AND scope = 'ip'` (or `AND failures = 0 OR locked_until > 0`
semantics) to the sweep so user counters are only reaped once their lockout has genuinely
elapsed; or change FR-7.2 and the comment to state that the username counter *is* reaped after an
idle window. Pick one; do not leave the comment and the SQL disagreeing.

## Minor

- **M1 — the "live leak" framing survives in one comment.**
  `apps/backend/internal/provider/github/authorize_git_secret_test.go:23-24` still reads "is the
  end-to-end guard for **the hosted leak**". The body immediately below (`:29-32`) carries the
  corrected framing ("Production never puts a token in argv … this test simulates the general
  case"), and `execution-notes-6.md:33` records the correction properly, so this is the last
  sentence holding the stronger claim. Commit `5fcb4cf`'s message body ("one reaching git's
  stderr was logged in clear") also states it, but commit messages are immutable history and the
  branch carries its own correction; I would not rewrite them. Reword the one test comment to
  "defence in depth for a per-user credential surfacing in git's diagnostics".
- **M2 — misplaced doc comment.** `internal/config/config.go:220-223` is `loadProviders`'
  documentation but sits immediately above `modeVar`. Introduced on this branch by inserting
  `modeVar` between the comment and its function.
- **M3 — production-dead `auth.Store.SaveAttempt`.** `internal/auth/store.go:365-373` is the
  non-atomic upsert that commit `32f668e` superseded with `UpdateAttempt`
  (`store.go:382-417`). Its only remaining caller is `store_test.go:323`, seeding a row. Leaving
  an exported, non-transactional writer beside the transactional one invites a future caller to
  reintroduce the lost-update race the commit fixed. Unexport it, or document it as
  test-seeding-only in one line.
- **M4 — production-dead `auth.NewUserProvider`.** `internal/auth/model.go:188-209` validates the
  slug and the required fields, but the only production writer,
  `Service.CreateProvider` (`internal/auth/providers.go:70-77`), builds the struct literal
  directly (it can, same package). So the constructor's invariants guard the tests and not the
  code. Have `CreateProvider` go through it.
- **M5 — `DeleteOtherLoginSessions` fails open on a nil `keep`.**
  `internal/auth/store.go:185-192` uses `token_hash != ?`; with a nil `[]byte` that parameter
  binds to SQL NULL, `x != NULL` evaluates to NULL, and **no rows are deleted** — the opposite of
  "revoke every other session". Unreachable today: `ChangePassword` is an authenticated route, so
  `tokenHashFrom` (`internal/api/authctx.go:39-44`) always returns the hash `authenticate`
  attached. Worth making fail-closed anyway, since the defensive direction here is "delete
  everything" and the current direction is "delete nothing".
- **M6 — a database fault looks like a bad password.** `Service.Login`
  (`internal/auth/service.go:174-192`) treats any non-nil `lookupErr` — not just `ErrNotFound` —
  as invalid credentials, and records a throttle failure for it. A transient store error during
  a SQLite hiccup therefore both lies to the user and pushes their username and IP toward a
  lockout. Distinguish `ErrNotFound` from everything else and return the real error for the rest.
- **M7 — `validationError` echoes internal error text.**
  `internal/api/settings_providers.go:184` sends `err.Error()` to the client, which includes the
  `auth: ` package prefix and the `: invalid input` wrapper suffix. No value or secret leaks (the
  four `ErrInvalidInput` messages are deliberately value-free), so this is cosmetic contract
  noise only.
- **M8 — the end-to-end log test runs below the level it most wants to observe.**
  `internal/app/secrets_test.go` sets `LOG_LEVEL=info`, but the git-stderr line it is implicitly
  protecting is logged at `Debug` (`internal/gitx/exec.go:190`). The test is still valuable — it
  exercises the real production-wired logger across register/login/provider-create/review-create
  and its own doc comment is unusually honest about what it does and does not prove — but the
  gitx path is covered by `internal/gitx/spec_secrets_test.go` and the two provider tests, not by
  this one. Raising it to `debug` would close the gap for free.
- **M9 — `CLAUDE.md:59-61` grants an import that production does not use.** It says
  `internal/auth` "may import `db`". `go list -deps ./internal/auth` shows it does not:
  `store.go` takes a bare `*sql.DB` and only `_test.go` files reach `internal/db`. This is a
  permission, not a crossed boundary (recorded residual #7), but describing what the code
  actually does is stronger: `auth` takes a `*sql.DB` and never names the `db` package.
- **M10 — native `<select>`.** `UserProviderForm.tsx:103-110` and `:232-239` use a native select
  rather than the shipped Radix `Select`. Recorded residual #4; a UI-consistency call that belongs
  to the frontend reviewer, and my I2 is the larger issue in the same file.

## The `internal/auth` flake — explicit call

**The evidence does not retire it, and I can name a plausible mechanism. It is not a merge
blocker.**

The prime suspect is
`apps/backend/internal/auth/service_test.go:224-257`,
`TestLoginIsIndistinguishableBetweenUnknownUserAndWrongPassword`. It is a **wall-clock timing
assertion**: three rounds of (unknown-username login, wrong-password login), then
`if um < wm/2 || wm < um/2 { t.Fatalf(...) }` on the medians. Everything about its environment
works against it:

- it calls `t.Parallel()` (`:225`), so it runs alongside the rest of the package;
- each `Login` performs a 64 MiB Argon2id verify, and `auth.Service` admits only four at a time
  (`hashConcurrency = 4`, `internal/auth/service.go:22, 93-103`), so a sample's latency includes
  time spent queueing behind *other parallel tests'* hashes;
- the suite runs under `-race`, which inflates and destabilises those timings further;
- a median of three is a single-sample defence — one stalled round shifts it outright.

That profile matches the reported sighting precisely: seen twice under full-suite load by
different agents, never reproduced in a focused `./internal/auth/ -count=3` run, because a
focused run removes exactly the contention that makes it fail. ~20 clean runs do not retire a
load-dependent 2x ratio check; they bound its rate, which is all they can do.

Secondary suspect, same class: `internal/auth/password_test.go:147-167` gives
`VerifyPassword` a 200 ms wall-clock deadline to reject an oversized-parameter hash. The
function should return in microseconds, so the margin is large — but it is still a wall-clock
deadline in a parallel, `-race`, 64-MiB-allocating package.

I found no shared-mutable-state mechanism: each fixture gets its own store and its own temp
database, the stubs guard their state with mutexes (`service_test.go:36, 63`), and `now` is
injected rather than read from the wall clock for anything functional. So this is a test-harness
robustness issue, not a product race — which is the good version of this answer, but it should be
fixed rather than left to re-surface in CI. Recommended follow-up (a ticket, not a merge gate):
make the timing test load-independent — count Argon2id invocations through an injected hook
instead of measuring them, or at minimum drop `t.Parallel()` from it, widen the ratio to 4x, and
take a median of five.

## Residual triage

| # | Item | Verdict |
|---|------|---------|
| 1 | Credential-injection path unexercised (`fake.AuthorizeGit` is a no-op) | **Still fine, do not block.** I verified the code answers it rather than a test: `grep` finds no production reader of `ExitError.Result.Stderr`, and `internal/gitx/exec.go:159-190` logs only category/repo/session/exit/duration/redacted-stderr — never `Spec.Args` or `Spec.Env`. The ruling's stated cost (a future logger of `Args`/`Env` goes uncaught) is real; a ticket for a one-assertion guard is proportionate. |
| 2 | `gitx.ExitError.Result.Stderr` is a raw exported field | **Still fine; ticket the `LogValue()`.** Zero production consumers today. Worth noting the stakes have changed slightly under hosted mode: the app-level `redactingHandler` is built from `config.Config.Secrets()` (`internal/app/app.go:199`), which *cannot* contain a per-user token, so if a caller ever does pass this to `slog`, hosted tokens go through unredacted where env-configured ones would not. Follow-up, not a blocker. |
| 3 | GitLab `spec_secrets_test.go` filename asymmetry | **Still fine.** Naming only. The mechanism is shared in one place (`exec.go:109-120`) and the GitLab declaration has its own guard (`gitlab/authorize_git_secret_test.go:17-43`). |
| 4 | Native `<select>` instead of Radix `Select` | **Frontend reviewer's call** (my M10). I would not block on it. My I2 — the duplicated form body in the same file — is the finding I would fix first. |
| 5 | `renderPageWithClient` duplicates `render.tsx` | **Still fine.** Test-helper duplication, no behavioural risk. |
| 6 | Task 22 mutation coverage non-exhaustive | **Still fine.** Coverage breadth, not a defect. |
| 7 | `CLAUDE.md:60` says `auth` "may import `db`" | **Tighten the wording** (my M9). Confirmed by `go list -deps` that production `auth` does not import `db`. Not a blocker; it is a permission statement, but describing the actual shape is better documentation. |
| 8 | `converge-cli` shares `app.New` and would honour inherited `CONVERGE_MODE=hosted` | **Ruling no longer matches the code — see I1.** The code was fixed in `fc657d4`; the doc was not updated. This is the one residual I am reversing. |

### Narrative correction

Applied in the right places (`execution-notes-6.md:33` states the defence-in-depth framing
plainly), with one survivor: the "the hosted leak" phrasing at
`internal/provider/github/authorize_git_secret_test.go:23-24` (my M1).

## Coherence across the 50 commits

I looked specifically for abandoned half-migrations, two-ways-of-one-thing, silently dropped
patterns, and dead code from superseded approaches.

- **No abandoned migrations.** The `Registry → Resolver` change (`461ea8b`) is complete: no
  production call site reaches `Registry.Get` directly any more; `App.Registry` survives only as
  the thing the static resolver wraps (`internal/app/app.go:264-281`), which is legitimate.
  Scope threading (`efc8e3d`, `0494dc6`, `e145057`) reaches every seam — `session.Store`,
  `mirror.Cache`, `review.Service`, and both binaries — with no "TODO: thread scope here"
  remnants. No `TODO`/`FIXME`/`XXX`/`HACK` markers anywhere in the changed Go or TS (the four
  `grep` hits are `xxx` inside test token fixtures).
- **Two superseded-approach remnants**, both exported and both now test-only: `SaveAttempt`
  (M3) and `NewUserProvider` (M4). Neither is harmful today; both are the shape that decays.
- **One pattern applied consistently**, worth crediting: every new guard fails closed
  (`resolver.go:64-70`, `store.go:286-288`, `cache.go:105-117`, `crypt.go:64-66`,
  `settings_providers.go:182-188`, `password.go:138-143`). That is not an accident of review; it
  is visible as a convention.
- **The fix commits read as genuine fixes, not churn**: `32f668e` (atomic counter), `01cf6e2`
  (Argon2 ceilings), `13bea39` (backslash in `safeNext`), `a872dea`
  (UNAUTHENTICATED vs INVALID_CREDENTIALS on 401), `7637842` (fail closed on unrecognised
  provider-settings errors), `799f3a3` (retry Purge cleanup via Sweep). Each closes something a
  reviewer would otherwise have found here.

## Adversarial attempts that found nothing

Recording these so a reader knows what was actually tried rather than assumed.

- **Cross-tenant session access**: `Get`/`List`/`Files`/`FileDiff`/`CombinedDiffPath`/`Finish` all
  take a scope and funnel through `Store`'s `scope.Matches(owner)` filter
  (`internal/review/service.go:88-105, 588-636`, `internal/session/store.go:186-222`).
  `StartBuild` deliberately uses the standalone scope for its lookup (`service.go:217-221`) but
  then derives the acting scope from the *persisted owner* via `scopeOf`
  (`service.go:413-425`), so a background build cannot be made to act as another user.
- **Cross-tenant provider access**: every read and write is `WHERE user_id = ?`
  (`internal/auth/store.go:266-331`), `DeleteProvider` asks `ProviderInUse` with
  `identity.ForUser(userID)` not the request scope (`internal/auth/providers.go:169`), and the
  resolver caches per user id with invalidation on every write path
  (`internal/auth/resolver.go:88-94`, called from `providers.go:81, 155, 178` and
  `service.go:321`).
- **Mirror crossing**: `PurgeUser("")` is double-guarded (`Store.Purge` errors, and
  `NamespaceFor(ForUser(""))` yields the root namespace which `PurgeNamespace` refuses) —
  `internal/review/service.go:643-655`.
- **CSRF / origin**: `originGuard` (`internal/api/authmw.go:40-61`) runs before `authenticate`,
  exempts only GET/HEAD and non-`/api/` paths, and requires either
  `Sec-Fetch-Site: same-origin` or an `Origin` whose host equals `r.Host`. A request with neither
  header is rejected — fail closed. Combined with `SameSite=Lax` this is the whole defence, as
  FR-4.4 states.
- **Open redirect**: `safeNext` (`apps/frontend/src/lib/api/formErrors.ts:11-17`) rejects
  anything not starting `/`, anything starting `//`, and anything containing a backslash;
  `URLSearchParams` decodes `%5C` before that check, so the percent-encoded bypass is covered.
- **Cookie**: `HttpOnly`, `SameSite=Lax`, `Secure` whenever `r.TLS != nil` or
  `CONVERGE_SECURE_COOKIES=true`, `MaxAge` mirroring absolute expiry
  (`internal/api/authctx.go:69-81`). Only the SHA-256 is persisted
  (`internal/auth/service.go:105-121`, `0001_init.sql:20`).
- **Throttle key forgery**: `X-Forwarded-For` is honoured only under
  `CONVERGE_TRUSTED_PROXY` and then only its rightmost hop
  (`internal/api/authctx.go:98-112`) — the correct choice, since that is the entry the nearest
  trusted proxy appended. The reasoning is also documented for operators
  (`docs/hosted-mode.md:27-33`).
- **Path-prefix evasion of `authenticate`**: `//api/reviews` and `/api/../api/reviews` are
  cleaned-and-redirected by `http.ServeMux` rather than served, and a path that fails the
  `/api/` prefix check is served by the UI handler, not by a review handler. `publicRoute`
  (`internal/api/authmw.go:20-29`) matches exact paths, so any near-miss requires authentication
  — fail closed.

## Could not verify

- **The gates themselves.** I read `task-28-gate.log` rather than running them, per the brief.
  The log is internally consistent (timestamps advance, real tool output, five explicit
  `EXIT_CODE=0` markers, a real multi-stage Docker export), so I treat it as credible evidence —
  but it is evidence about a tree state, and my finding-fix edits will need a re-run.
- **Live provider behaviour.** Every GitHub/GitLab interaction on this branch is exercised against
  `httptest` stand-ins. The credential-injection path in particular is verified structurally
  (env construction, secret declaration) rather than against a real remote; residual #1 is the
  recorded form of that gap and `docs/manual-checklist.md` carries the manual sweep.
- **The flake.** I did not attempt a reproduction run (instructed not to). My call above is a
  code-level mechanism argument, which I believe is the stronger artefact, but it is an argument.
