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
