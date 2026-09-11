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
