# task-002-theme-mode-selection — Execution Context

Companion to `plan.md`. Everything here was verified against source in this
worktree, not recalled. File references are repo-relative.

---

## 1. Where the work happens

- Worktree: `.worktrees/task-002-theme-mode-selection`, branch
  `task-002-theme-mode-selection`. Reuse it; never create another.
- **Every file this plan touches is under `apps/frontend`.** No Go source is
  edited. The only backend-adjacent effect is that `npm run build` regenerates
  `apps/backend/internal/ui/dist`, which is gitignored (`.gitignore:45`; only
  `.gitkeep` is tracked) — so build output is never staged.
- Node may not be on `PATH`:
  `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.

## 2. Why the palette does nothing today

`apps/frontend/src/index.css` already has the complete picture except the
trigger:

- `@custom-variant dark (&:is(.dark *))` (line 6) wires Tailwind's `dark:`
  variant to a `.dark` ancestor class.
- A full `:root` token block and a full `.dark` token block, both stock shadcn
  neutral values.
- `html, body, #root { height: 100% }` — which is why `AppShell`'s `min-h-full`
  resolves.

Nothing ever adds `dark` to `document.documentElement`. That one missing class is
the entire bug. `:root { color-scheme: light dark; }` currently advertises a
choice the user cannot make.

## 3. Key files, as they stand

| File | State today |
| --- | --- |
| `apps/frontend/src/App.tsx` | `QueryClientProvider` → `BrowserRouter` → `AppRoutes` + bare `<Toaster richColors position="top-right" />`. Module-level `const queryClient = createQueryClient()`. |
| `apps/frontend/src/routes.tsx` | Four `<Route>`s: `/`, `/select`, `/reviews/:id`, `*`. Pure route table, no layout route. |
| `apps/frontend/index.html` | Minimal. `<head>` holds only charset, viewport, title. One `<script type="module" src="/src/main.tsx">` at the end of `<body>`. |
| `apps/frontend/src/test/setup.ts` | `jest-dom` import plus `afterEach(cleanup)`. Nothing else. |
| `apps/frontend/src/test/render.tsx` | `renderWithProviders` = `QueryClientProvider` → `MemoryRouter`. `queryWrapper` for hook tests. |
| `apps/frontend/src/components/features/review/FileDiff.tsx` | Builds an `options` object with `diffStyle`, `expandUnchanged`, `collapsedContextThreshold`, `overflow`. `key={path}`. No theme. |
| `apps/frontend/src/components/ui/` | 8 vendored shadcn components. No `dropdown-menu.tsx`. |

## 4. Decisions already made (do not relitigate)

| Decision | Reason |
| --- | --- |
| `resolved` is derived during render, never stored | Makes FR-5.2 free and makes preference/resolved disagreement unrepresentable |
| One always-on media-query listener, empty dep array | Avoids re-registering on every preference change; cleanup still satisfies FR-5.3 |
| `useLayoutEffect`, not `useEffect`, for `applyTheme` | Commits in the same frame as the click; no painted frame in the wrong theme |
| `classList.toggle("dark", …)`, never `className =` | FR-2.4 (unrelated root classes survive) holds by construction |
| `color-scheme` written in **both** CSS and inline style | CSS is the no-JS floor; the inline style covers the pre-stylesheet window. Both derive from the same resolved value, so they cannot disagree |
| Boot script is a **classic** inline script | `type="module"` is deferred and always runs after first paint |
| Six small files under `src/lib/theme/` + `src/components/theme/` | `react-refresh/only-export-components` forbids mixing a component and a non-component export; pure functions must be testable without a renderer; the FR-4.4 sync comment points at a 5-line file |
| `AppShell` is a wrapper in `App.tsx`, not a layout route with `<Outlet />` | Applies to every path including `*`, needs no route data, leaves `routes.tsx` and `routes.test.tsx` structurally untouched (FR-7.3) |
| Header is `sticky`, not `fixed`, with no `max-w-*` | Occupies layout space instead of overlaying the diff; each page keeps its own container (`ReviewPage` uses `max-w-7xl`) |
| Wordmark is `<Link to="/">` | Open question 2. Three-step wizard, "start over" is real, conventional expectation |
| `DropdownMenuRadioGroup` / `RadioItem`, not plain items | Radix emits `role="menuitemradio"` + `aria-checked`, so "marked as selected" is announced and assertable by role rather than by sniffing for a check icon |
| Trigger icon from `resolved`, checked item from `preference` | They differ under System — that is the point of FR-1.5 |
| `pierre-light` / `pierre-dark` for the diff, not a Shiki theme | Open question 1. The library's own matched pair; `pierre-light` is what renders today, so light mode is visually unchanged |
| `PatchDiff`'s `key` stays `{path}` | `areOptionsEqual` + `setOptions` already re-theme in place; keying on the theme would throw away the parsed diff and the highlighter cache |
| `themeType` never receives `"system"` | It would follow the OS instead of the app *and* suppress the shadow-root `color-scheme` declaration |
| No `storage` event listener | Cross-tab sync is not a requirement; one tab silently overriding another is worse than no sync |
| §4.9 audit is a verification exercise | The greps were already run at design time: zero findings |

## 5. Upstream facts, verified in `node_modules`

| Claim | Evidence |
| --- | --- |
| `radix-ui` re-exports the dropdown primitive | `node_modules/radix-ui/dist/index.d.ts:21-22` — `import * as reactDropdownMenu from '@radix-ui/react-dropdown-menu'; export { reactDropdownMenu as DropdownMenu }`. Same package `select.tsx` imports from |
| `@radix-ui/react-dropdown-menu` is installed | Present in `node_modules`; a transitive dep of `radix-ui@1.6.7`. **No new dependency is needed** |
| Radix's dropdown needs `ResizeObserver` | `react-dropdown-menu` → `react-menu` → `react-popper` → `react-use-size`, which does `new ResizeObserver(...)` at `node_modules/@radix-ui/react-use-size/dist/index.mjs:12`. jsdom has none |
| Radix's menu needs nothing else jsdom lacks | Grepped `@radix-ui/react-menu/dist/index.mjs` for `scrollIntoView`, `hasPointerCapture`, `requestAnimationFrame` — zero hits |
| `@pierre/diffs` is at 1.4.1 and ships its own theme pair | `node_modules/@pierre/diffs/package.json`; `dist/constants.js:28-31` → `{ dark: "pierre-dark", light: "pierre-light" }` |
| `themeType` / `theme` are real options | `dist/types.d.ts:334` `theme?: DiffsThemeNames \| ThemesType`, `:337` `themeType?: ThemeTypes`, `:51-52` `DiffsThemeNames = BundledTheme \| (string & {})`, `ThemesType = Record<'dark' \| 'light', DiffsThemeNames>` |
| `sonner`'s `theme` prop | `node_modules/sonner/dist/index.d.ts:100` — `theme?: 'light' \| 'dark' \| 'system'` |
| Lucide icons exist | `lucide-react@1.41.0` exports `Sun`/`SunIcon`, `Moon`/`MoonIcon`, `Monitor`/`MonitorIcon`. Use the `Icon` suffix to match `select.tsx`'s `ChevronDownIcon` convention |
| `Button` has the variants the picker needs | `src/components/ui/button.tsx` — `ghost` variant, `icon` size (`size-8`) |

## 6. Tooling constraints that bite

- `eslint.config.js` ignores `dist` and `src/components/ui/**`, and enables
  `react-refresh/only-export-components` (warn) plus
  `reactHooks.configs.flat["recommended-latest"]`.
- `.prettierignore` lists `dist`, `node_modules`, `src/components/ui`.
  **`index.html` is not ignored** — run `npm run format` after editing it, or
  `npm run format:check` fails.
- `src/components/ui/**` is exempt from lint and format but **not** from `tsc`.
  `npm run build` is the only gate that typechecks it.
- `vitest.config.ts`: `jsdom`, `globals: true`, `setupFiles:
  ["./src/test/setup.ts"]`, `include: ["src/**/*.test.{ts,tsx}"]`, `@` aliased to
  `./src`.
- MSW is started **per test file** (`beforeAll(() => server.listen(...))`), not
  globally. A test that renders a fetching route without starting the server hits
  the network — which is why `App.test.tsx` points jsdom at an unmatched path.
- jsdom shares one `document` per test file. Without the new `afterEach` reset in
  `setup.ts`, a test that goes dark leaks into the next one.

## 7. Existing tests that must be touched

All 97 current tests pass before this plan starts and must pass after every task.
`useTheme` throws outside a provider, so two files that build their own render
trees need the provider added. **No assertion in either changes** (FR-7.3):

- `src/components/features/review/__tests__/FileDiff.test.tsx` — calls bare
  `render(...)`; moves to `renderWithProviders`. Keep its standing `NOTE:`
  comment: never assert on `@pierre/diffs`' rendered output, which lives in a
  shadow root and needs `ResizeObserver`. Assert on the props Converge passes.
- `src/__tests__/routes.test.tsx` — hand-built `QueryClientProvider` +
  `MemoryRouter` tree gains `ThemeProvider`.

Page tests render pages directly through `renderWithProviders`, not through
`App`, so the new shell does not affect them.

## 8. Dependency order

```
1 harness ──► 2 core ──► 3 provider ──► 4 render helpers ──┬──► 6 picker ──► 7 shell ──► 8 App wiring
                                                           └──► 9 FileDiff (needs 3; its
                                                                re-theme test needs 6)
                         5 boot script + CSS (needs 2's THEME_STORAGE_KEY only)
                                                                            10 audit + full CI (last)
```

Task 5 is independent of 3–9 and can be done any time after Task 2. Task 9's
in-place re-theme test renders `ThemeToggle`, so run it after Task 6. Task 10 is
strictly last.

## 9. Verification commands

Frontend (cwd `apps/frontend`), per task:

```sh
npm test && npm run lint && npm run format:check && npm run build
```

`npm run build` is load-bearing beyond bundling: it is the only step that runs
`tsc`, and it rewrites the embedded `apps/backend/internal/ui/dist`.

Repository root, once in Task 10:

```sh
make lint && make test && make test-integration && make build && make docker-build
```

Give these an explicit generous timeout; `make docker-build` is slow. Never
report a command as passing without having seen its output.

## 10. Out of scope

No backend or API change. No cross-device or cross-tab sync. No palette
redesign — if the audit finds a *token-value* contrast problem, record it as a
follow-up rather than repaletting. No themes beyond light and dark, no per-page
overrides, no diff-theme selection (the 65-theme Shiki catalog stays
unexposed), and no theming of surfaces that do not yet exist.

## 11. Known rough edges, accepted

- Switching modes with the review page open may show the previous theme for a
  frame or two: `@pierre/diffs` loads Shiki themes asynchronously. Internal to
  the library, affects only the diff surface, not worth working around.
- `StrictMode` double-invokes effects in development. `applyTheme` is idempotent
  and the media-query effect has symmetric cleanup, so this is harmless — but it
  is why the cleanup test asserts on `removeEventListener` having been called
  rather than on an exact call count.
- Two carried-forward token observations, recorded as follow-up candidates rather
  than fixed here: `--destructive` on dark (`oklch(0.704 0.191 22.216)` on
  `oklch(0.145 0 0)`) and `--muted-foreground` (`oklch(0.708 0 0)`) on `--muted`
  (`oklch(0.269 0 0)`). Both are stock shadcn values.
