# Plan Audit — task-002-theme-mode-selection

**Plan Path:** `docs/tasks/task-002-theme-mode-selection/plan.md`
**Audit Date:** 2026-09-10
**Branch:** `task-002-theme-mode-selection`
**Base (code):** `b348d0b` → `0473d92` (13 commits; the 3 earlier commits are PRD/design/plan docs)
**Scope of this audit:** whole-branch plan adherence. Per-task spec+quality reviews already ran and returned Approved; CI (`make lint/test/test-integration/build/docker-build` + the frontend gate) is green at `0473d92` and was **not** re-run here.

## Executive Summary

All ten plan tasks are implemented; every functional requirement in the plan's "Requirement coverage" table has locatable implementation. Zero Go files changed, matching the plan's Global Constraints. No Critical findings. One Important finding: the pre-paint boot script — the mechanism behind the headline "no flash of wrong theme" acceptance criterion — has **no behavioral verification at all**; its only guard is a string-matching drift test, and the manual browser check that would have covered it is (correctly) recorded as unperformed. Four Minors, none blocking. Recommendation: **READY_TO_MERGE**, with the Important finding tracked as a fast follow-up (or closed pre-merge — it is ~40 lines of test).

## Requirement Coverage

| FR | Verdict | Evidence |
|---|---|---|
| FR-1.1 three modes | MET | `apps/frontend/src/lib/theme/types.ts:2` (`ThemePreference`), `:25-27` (`isThemePreference` allowlist) |
| FR-1.2 two resolved themes | MET | `apps/frontend/src/lib/theme/types.ts:5` |
| FR-1.3 resolution rule | MET | `apps/frontend/src/lib/theme/apply.ts:23-26`; tested `src/lib/theme/__tests__/theme.test.ts` (`resolveTheme` block) |
| FR-1.4 matchMedia absent/throws ⇒ light | MET | `apply.ts:9-20` (`darkMediaQuery` try/catch + `?? false`); tested `theme.test.ts:50` and ThemeProvider "survives a missing matchMedia" |
| FR-1.5 both values readable | MET | `apps/frontend/src/lib/theme/context.ts:4-11`; consumed at `ThemeToggle.tsx:21` |
| FR-2.1 `dark` class on root | MET | `apply.ts:38-41`; `ThemeProvider.tsx:25-27` |
| FR-2.2 `color-scheme` on root | MET | `apply.ts:41`; stylesheet floor split at `src/index.css` `:root { color-scheme: light }` / `.dark { color-scheme: dark }` (diff `b348d0b..HEAD -- src/index.css`) |
| FR-2.3 synchronous same-frame update | MET | `ThemeProvider.tsx:25-27` uses `useLayoutEffect`, not `useEffect` |
| FR-2.4 preserves other root classes | MET | `apply.ts:40` `classList.toggle("dark", …)`; tested "preserves unrelated classes already on the root" |
| FR-3.1 key `converge.theme` | MET | `types.ts:15`; boot script `index.html:12` |
| FR-3.2 absent ⇒ system | MET | `storage.ts:12-19` |
| FR-3.3 corrupt ⇒ system | MET | `storage.ts:15` via `isThemePreference`; tested for `"DARK"`, `"twilight"`, `""`, `"{}"` |
| FR-3.4 throwing store never breaks | MET | `storage.ts:13-18`, `:26-31`; tested "still applies the theme when persistence is unavailable" |
| FR-3.5 persists literal `system` | MET | `storage.ts:25-27` writes the preference verbatim; `ThemeProvider.tsx:37-40`; tested "persists the literal 'system', not the theme it resolved to" |
| FR-4.1 inline render-blocking script, first in `<head>` | MET | `apps/frontend/index.html:4-25` — classic `<script>` with no `type`/`defer`/`async`, first child of `<head>` |
| FR-4.2 self-contained, try/catch, never throws | MET | `index.html:11-23`. See Important-1 for a scoping caveat |
| FR-4.3 provider adopts, no flicker | MET | `apply.ts:38-41` is idempotent; `ThemeProvider.tsx:25-27` re-applies in a layout effect |
| FR-4.4 duplication guarded | MET (partially) | Paired comments `types.ts:7-14`, `apply.ts:28-37`, `index.html:5-9`; drift test `src/__tests__/bootThemeScript.test.ts`. Guard strength — see Minor-1 |
| FR-5.1 live OS tracking under system | MET | `ThemeProvider.tsx:29-35`; tested "follows a live OS change while on system" |
| FR-5.2 OS inert under light/dark | MET | falls out of `apply.ts:23-26` + derived-during-render `ThemeProvider.tsx:20`; tested for both pinned modes |
| FR-5.3 listener torn down via effect cleanup | MET | `ThemeProvider.tsx:34`; tested "tears the media-query listener down on unmount" |
| FR-6.1 icon-trigger dropdown, 3 items, sun/moon/monitor | MET | `ThemeToggle.tsx:24-51`; icons `:1`, `:38/:42/:46` |
| FR-6.2 current preference marked | MET | `ThemeToggle.tsx:32` (`value={preference}`) → Radix `menuitemradio`/`aria-checked`; tested "marks the stored preference as checked" |
| FR-6.3 select sets/applies/persists/closes | MET | `ThemeToggle.tsx:33-35` → `ThemeProvider.tsx:37-40`; tested "applies, persists, and closes the menu on selection" |
| FR-6.4 accessible name + keyboard | MET | `ThemeToggle.tsx:26` `aria-label="Change theme"`; keyboard and Escape tests in `ThemeToggle.test.tsx` |
| FR-6.5 trigger reflects `resolved` | MET | `ThemeToggle.tsx:27`; tested "keeps System checked while the trigger shows the resolved theme" |
| FR-6.6 vendored `dropdown-menu` | MET | `src/components/ui/dropdown-menu.tsx` (hand-written fallback form from the plan, `radix-ui` re-export, same shape as `select.tsx`) |
| FR-7.1 persistent top bar | MET | `src/components/layout/AppShell.tsx:14-24` |
| FR-7.2 all routes inside the shell | MET | `src/App.tsx:27-29` wraps `AppRoutes`; `main.tsx:13` renders `App` |
| FR-7.3 no URL/route changes; tests only accommodate the wrapper | MET | `routes.test.tsx` diff adds only the `ThemeProvider` wrapper (6 insertions, 3 deletions, zero assertion changes); `routes.tsx` untouched |
| FR-7.4 no duplicate `PageHeader` | MET | `AppShell.tsx` renders no title; `PageHeader.tsx:11` still owned by pages |
| FR-7.5 does not crowd the review page | MET | `AppShell.tsx:16` `sticky`, not `fixed`; no `max-w-*` on the shell |
| FR-8.1 Toaster gets resolved theme | MET | `App.tsx:17-20`; tested `App.test.tsx:40-54` (parameterised over all three stored values, mocking `sonner` so the assertion is on the prop, not on sonner's own resolution) |
| FR-8.2 theme via existing `options` prop | MET | `FileDiff.tsx:33-43` |
| FR-8.3 `themeType` = resolved, never `"system"` | MET | `FileDiff.tsx:41` + `:15` (hook read above the `if (binary)` early return); tested `FileDiff.test.tsx:91-99` |
| FR-8.4 concrete light/dark theme names | MET | `FileDiff.tsx:42` `{ light: "pierre-light", dark: "pierre-dark" }`; asserted `FileDiff.test.tsx:82` |
| FR-8.5 re-theme in place | MET (design-verified) | `key={path}` unchanged at `FileDiff.tsx:31`; `FileDiff.test.tsx:111-125` proves the new `themeType` reaches the component without a remount of the surrounding tree. Whether the real `PatchDiff` re-themes without discarding its parse is design analysis (`plan.md` Task 9) + manual checklist item 7, not machine-verified |
| FR-9.1 audit for non-semantic color | MET | Re-ran all three greps independently this audit — grep 1: no matches; grep 2: only `#421`/`#435` PR numbers in test fixtures; grep 3: only the two `dark: "pierre-dark"` object-key false positives. Matches `audit-dark-mode.md` exactly |
| FR-9.2 convert or give `dark:` counterpart | MET (vacuously) | Zero occurrences to convert; confirmed by the greps above |
| FR-9.3 severity colors keep contrast in dark | MET — and now measured, see below | `badge.tsx:14-15`, `ReviewErrorPanel.tsx:18`, `ReviewStatus.tsx:28` |
| FR-9.4 audit recorded | MET | `docs/tasks/task-002-theme-mode-selection/audit-dark-mode.md` (159 lines, commands + output + follow-ups) |
| Acceptance: no flash, WCAG AA | PARTIAL | Contrast is now computed (below). No-flash remains an open human checklist item — see Important-1 |
| Acceptance: every CI command clean | MET | Green at `0473d92` per controller |
| Acceptance: code review before PR | MET | This document |

**Coverage: 41/41 FRs implemented. 0 unimplemented.**

## Cross-Task Integration Trace

Traced the full path end to end. It meets at every seam:

1. **Stored preference → provider.** `readStoredPreference()` (`storage.ts:12`) is the lazy initialiser at `ThemeProvider.tsx:17`; `prefersDark()` (`apply.ts:18`) at `:18`.
2. **Provider → `resolved`.** Derived during render at `ThemeProvider.tsx:20`, never stored, so `preference` and `resolved` cannot disagree. Exposed via memoised context at `:42-45`.
3. **Consumer 1 — root `dark` class.** `useLayoutEffect` → `applyTheme` (`ThemeProvider.tsx:25-27` → `apply.ts:38-41`).
4. **Consumer 2 — `ThemeToggle` trigger icon.** `ThemeToggle.tsx:21` destructures `resolved`; `:27` selects moon/sun. The checked item reads `preference` (`:32`) — the two deliberately differ under System, and a test pins that split.
5. **Consumer 3 — `sonner` `Toaster`.** `ThemedToaster` (`App.tsx:17-20`) reads `resolved` and is mounted *inside* `ThemeProvider` at `App.tsx:30`.
6. **Consumer 4 — `@pierre/diffs` `PatchDiff`.** `FileDiff.tsx:15` reads `resolved`; forwarded at `:41`.

Provider nesting is consistent between production (`App.tsx:24-33`: Query → Theme → Router → Shell) and the test harness (`src/test/render.tsx:42-46`: Query → Theme → MemoryRouter), so `Link` inside `AppShell` resolves in both. No consumer of `useTheme` is reachable outside a provider: every test that mounts one goes through `renderWithProviders`, `routes.test.tsx`'s updated tree, or `App` itself (verified by sweeping bare `render(` imports across `src/**/*.test.tsx`).

**Landmark check (a real candidate seam):** `AppShell` adds `<header>` + `<main>`, while `PageHeader.tsx:11` and `ReviewHeader.tsx:16` also render `<header>`. Those are descendants of `<main>` and therefore carry no `banner` role, so `AppShell.test.tsx`'s `getByRole("banner")` stays unambiguous. No conflict.

**`min-h-full` dependency:** `AppShell.tsx:15` relies on `html, body, #root { height: 100% }`, present at `src/index.css:44-47`. Holds.

## Boot Script ↔ TS Core Agreement (verified by execution, not inspection)

I extracted the inline script from `index.html` and executed it in a `node:vm` sandbox against a stubbed `localStorage` / `window.matchMedia` / `document`, comparing its output to a faithful port of `readStoredPreference` → `resolveTheme` → `applyTheme`, over the cross-product of 8 stored values × OS light/dark × matchMedia present/absent × storage working/throwing (64 combinations).

**Result: all 32 combinations where storage is readable agree exactly** on both the `dark` class and `style.colorScheme`. The two implementations are genuinely in sync today. The 32 storage-throwing combinations diverge in one narrow way — see Minor-2.

## Findings

### Important-1 — The pre-paint path has no behavioral verification

**Evidence:** `src/__tests__/bootThemeScript.test.ts` is entirely `expect(html).toContain(...)` over the raw file text (`:10, :14-17, :21-24, :44-45`). The script is never executed by any test. The manual counterpart — checklist item 4, "no white flash on hard reload with Dark stored" — is honestly recorded as unperformed (`audit-dark-mode.md:118-123`).

So the single mechanism delivering the feature's headline acceptance criterion is currently backed by neither a machine check nor a human check. That is the definition of "cannot be trusted until confirmed."

This is cheap to close, and it does not require a browser: the `node:vm` harness I used above is ~40 lines and gives real behavioral coverage of FR-4.1/FR-4.2/FR-1.4/FR-3.2/FR-3.3 for the pre-paint path, including the corrupt-value fallback that checklist item 5 currently leaves to a human. Recommended: add it as `src/__tests__/bootThemeScript.behavior.test.ts` alongside the existing string guard. (The remaining genuinely-visual items — actual absence of flash, legibility sweeps — correctly stay manual.)

### Minor-1 — The drift guard is one-directional

`bootThemeScript.test.ts` cross-links exactly one symbol to the TS core: `THEME_STORAGE_KEY` (`:4`, `:10`). Every other assertion is a literal string about `index.html` (`:21-24`). Consequence: if `apply.ts` changed how it writes the theme — e.g. `root.classList.toggle("dark", …)` → a `data-theme` attribute — `index.html` would be untouched, the test would still pass, and the two would have silently drifted. The guard catches drift *originating in `index.html`*, not drift originating in `apply.ts`. Task 5's fix round already strengthened the value-level assertions (`:23-24` reject `colorScheme = pref`), so the most-likely regression is covered; this is the residual gap.

### Minor-2 — Boot script's try/catch is coarser than the TS core's

`index.html:11-23` wraps the storage read, the matchMedia read, and the DOM write in one `try`. `storage.ts:13-18` scopes its `catch` to the storage read only. Divergence, confirmed by execution: with a throwing `localStorage.getItem` (blocked storage / sandboxed iframe) on a dark-OS machine, the boot script aborts and leaves the light default, while the React provider then resolves `system` → `dark` — producing exactly the light-then-dark flash the script exists to prevent. Narrow (requires blocked storage), degrades gracefully, and FR-4.2's "never throws" still holds. One-line fix if desired: initialise `var pref = "system"` and give the `getItem` its own `try`.

### Minor-3 — Dead surface across task boundaries

- `dropdown-menu.tsx:86` exports `DropdownMenuPortal`, which nothing imports — and `DropdownMenuContent` already portals internally at `:34`, so it is redundant twice over. Pre-ruled acceptable (vendored, lint/format-exempt directory); recorded for completeness only.
- `src/test/matchMedia.ts:23` exposes `darkQueryListeners.addEventListener`, which no test ever asserts on (only `count()` and `removeEventListener` are used). Task 1 built it for a consumer that later tasks did not need.
- `src/test/matchMedia.ts:41-42` `addListener`/`removeListener` no-ops — plan-mandated, unused.

### Minor-4 — `not.toContain("defer")` is scoped to a slice that includes the boot comment

`bootThemeScript.test.ts:36-40` computes `openingTag` as everything from the last `<script` before `localStorage.getItem`, which includes the script's leading comment block. So the assertion is correctly *not* tripped by an unrelated deferred script added elsewhere (contrary to the concern parked during Task 5) — but it *is* tripped by the words `type=`, `defer`, or `async` appearing in the boot comment, which is what forced the Task 5 comment reword. Fragile in a surprising direction; a regex on the tag itself (`/<script([^>]*)>/`) would be more precise.

## Deferred-Minor Triage

Source: `.superpowers/sdd/plan/deferred-and-rulings.md`. **None must be fixed before merge.**

| Deferred item | Verdict |
|---|---|
| T1 `addListener`/`removeListener` dead stubs | Ship. Folded into Minor-3 |
| T2 inline `type ThemePreference` import style | Ship. Cosmetic; passes lint and `format:check` |
| T2 `firstCall!` after `toBeDefined()` | Ship. Cosmetic |
| T5 `not.toContain("defer")` over-broad | Ship — and the concern is **overstated**: the assertion is scoped to the boot script's own tag slice, not the whole file. Re-filed accurately as Minor-4 |
| T6 report mischaracterised the Radix focus mechanism | Ship. A report-prose error, not a code defect; the shipped test (`ThemeToggle.test.tsx:95-106`) and its comment are correct |
| T7 `z-40` magic number | Ship. Only one stacking context exists today; introducing a z-scale for a single consumer is premature |
| T9 `toMatchObject` subset assertion | Ship. The object is small and fully enumerated at `FileDiff.test.tsx:103-108` |
| T9 inconsistent mutation-ratio table | Ship. Reporting sloppiness only |
| T10 `audit-dark-mode.md` omits CI results | Ship, but worth a one-line addendum — a reader opening only that doc sees no evidence CI was green. Doc-only |

The pre-ruled items (vendored `dropdown-menu.tsx`, `"node"` in `tsconfig.app.json`, T4's assertion-free shape, T10's document-only shape, the harness-injected extra co-author line) are all confirmed as described and are not findings.

## Machine-Verifiable Items Left Manual

You asked whether anything on the manual checklist should have been machine-checked. Two things:

**1. WCAG AA contrast (checklist item 9) is computable from the tokens — no browser needed.** The `oklch()` values in `src/index.css` fully determine it. Computed here (OKLab → linear sRGB, alpha composited in gamma space, WCAG relative-luminance ratio):

| Pair | Ratio | AA (4.5:1) |
|---|---|---|
| Dark: `text-destructive` on `dark:bg-destructive/20` over `--card` (destructive `Badge`) | **4.63:1** | PASS |
| Dark: `--foreground` on `bg-destructive/5` over `--background` (`ReviewErrorPanel`) | **18.17:1** | PASS |
| Dark: `--muted-foreground` on `--muted` | **5.83:1** | PASS |
| Light (pre-existing, unchanged by this branch): destructive `Badge` | 3.99:1 | fails, but ships today |
| Light (pre-existing, unchanged by this branch): `--muted-foreground` on `--muted` | 4.34:1 | marginal, ships today |

**Dark mode — the new surface this branch exposes — passes AA on every severity pair.** The two sub-4.5 values are light-mode pairs that predate this branch and are untouched by it. This closes FR-9.3 affirmatively and retires the "not measured — record ratio here" placeholders at `audit-dark-mode.md:147-150`. The design's two carried follow-up concerns (`--destructive` and `--muted-foreground` on dark) are, on these numbers, **not** dark-mode problems.

**2. Checklist item 5 (corrupt stored value falls back to System) is already machine-covered for the React path** (`theme.test.ts` corrupt-value block; ThemeProvider "treats a corrupt stored value as system") but **not** for the pre-paint path — which the harness in Important-1 would cover.

Everything else on the checklist (actual absence of flash on first paint, visual legibility sweeps, real browser + real OS toggle, real diff rendering) genuinely needs a human. Recording them as open was the right call.

## Build & Test Results

| Area | Build | Tests | Lint/Format | Notes |
|---|---|---|---|---|
| `apps/frontend` | PASS | PASS (163/163) | PASS | Per controller at `0473d92`; not re-run |
| `apps/backend` | PASS | PASS (18 pkgs) | PASS | Zero Go files changed — confirmed against `git diff --stat b348d0b..HEAD` |
| Repo (`make lint/test/test-integration/build/docker-build`) | PASS | PASS | PASS | Per controller at `0473d92`; not re-run |

Working tree verified clean (`git status --porcelain` empty) before and after this audit. No files were mutated; all experiments ran in `/tmp`.

## Overall Assessment

- **Plan Adherence:** FULL
- **Recommendation:** READY_TO_MERGE

Every task was implemented, no task was hollowed out, and the assembled system holds together at each of the four consumer seams. The Important finding is a verification gap in one narrow path, not a defect in it — the path itself was proven correct by execution during this audit.

## Action Items

1. **(Important, pre-merge or fast-follow)** Add a behavioral test for the pre-paint boot script: extract the inline `<script>` from `index.html`, execute it in `node:vm` (or jsdom) against stubbed `localStorage`/`matchMedia`/`document`, and assert the resulting `dark` class and `colorScheme` match `resolveTheme(readStoredPreference(), prefersDark())` across stored ∈ {light, dark, system, corrupt, absent} × OS ∈ {light, dark} × matchMedia ∈ {present, absent}.
2. **(Minor)** Give the boot script's `localStorage.getItem` its own `try`/`catch` so blocked storage does not also suppress the matchMedia branch (`index.html:11-23`).
3. **(Minor)** Strengthen the drift guard so it can detect drift originating in `apply.ts`, not just in `index.html` — e.g. assert the boot script contains the same literal class token that `applyTheme` writes, sourced from a shared exported constant.
4. **(Minor)** Replace the `openingTag` string slice in `bootThemeScript.test.ts:36-40` with a regex over the `<script …>` tag, so boot-script comment wording cannot trip the classic-script assertion.
5. **(Minor, doc)** Append the green CI result to `audit-dark-mode.md`, and replace the "not measured" contrast placeholders at `:147-150` with the computed ratios in this report.
6. **(Optional)** Drop the unused `DropdownMenuPortal` export and the unused `darkQueryListeners.addEventListener` handle, or leave both as pre-ruled.
