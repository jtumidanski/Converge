# Light / Dark / System Mode Selection — Design

Task: `task-002-theme-mode-selection`
Source PRD: `docs/tasks/task-002-theme-mode-selection/prd.md`
Status: Approved for planning
Created: 2026-09-10

---

## 1. Summary

The palette already exists; nothing applies it. This design adds a small,
purely client-side theme system in four layers:

1. **A pure core** (`src/lib/theme/`) — types, the storage key, resolution, and a
   single `applyTheme` DOM writer. No React, unit-testable without rendering.
2. **A React provider** that owns the preference, subscribes once to the OS media
   query, derives the resolved theme during render, and applies it in a layout
   effect.
3. **A pre-paint boot script** in `index.html` that is a hand-inlined copy of the
   core's logic, so the first frame is already correct.
4. **Consumers** — a dropdown picker in a new app shell, `sonner`'s `Toaster`, and
   `@pierre/diffs`' `PatchDiff`.

Two PRD assumptions were checked against source and changed the shape of the work:

- **The §4.9 dark-mode audit is a verification exercise, not a conversion
  exercise.** A full grep of `src/` finds zero hardcoded palette classes, zero
  hex/`rgb()` literals, and zero inline `style` colors. Every component already
  uses semantic tokens. See §9.
- **`@pierre/diffs` ships its own matched light/dark pair** (`pierre-light` /
  `pierre-dark`) and already sets `color-scheme` inside its shadow root from
  `themeType`. This answers open questions 1 and 4. See §7.

---

## 2. Module layout

| File | Kind | Responsibility |
| --- | --- | --- |
| `src/lib/theme/types.ts` | new | `ThemePreference`, `ResolvedTheme`, `THEME_STORAGE_KEY`, `DEFAULT_PREFERENCE`, `isThemePreference` |
| `src/lib/theme/apply.ts` | new | `prefersDark()`, `resolveTheme(pref, systemIsDark)`, `applyTheme(resolved)` |
| `src/lib/theme/storage.ts` | new | `readStoredPreference()`, `writeStoredPreference(pref)` — both total, never throw |
| `src/lib/theme/context.ts` | new | The context object and `ThemeContextValue` type |
| `src/lib/theme/useTheme.ts` | new | Consumer hook; throws outside a provider |
| `src/components/theme/ThemeProvider.tsx` | new | State, subscription, DOM application |
| `src/components/theme/ThemeToggle.tsx` | new | The picker |
| `src/components/layout/AppShell.tsx` | new | Persistent top bar + content slot |
| `src/components/ui/dropdown-menu.tsx` | new (vendored) | shadcn/Radix primitive |
| `src/App.tsx` | edit | Mount provider, shell, and theme-aware `Toaster` |
| `src/components/features/review/FileDiff.tsx` | edit | Forward theme into `PatchDiff` options |
| `index.html` | edit | Pre-paint boot script |
| `src/index.css` | edit | `color-scheme` split across `:root` / `.dark` |
| `src/test/setup.ts`, `src/test/matchMedia.ts`, `src/test/render.tsx` | edit / new | `matchMedia` stub, per-test DOM reset, provider in the render helper |

**Why so many small files.** Three forces push the same way. `eslint.config.js`
enables `react-refresh/only-export-components`, which warns when a module exports
both a component and a non-component — so the provider, the hook, and the context
object cannot share a file. The pure functions must be testable without a
renderer, so they cannot live in a `.tsx` component module. And `applyTheme` is
the one function the boot script mirrors verbatim; isolating it in
`apply.ts` makes the FR-4.4 "keep these two in sync" comment point at a
five-line file rather than a component.

**Rejected alternative:** one `ThemeProvider.tsx` exporting `ThemeProvider`,
`useTheme`, and the helpers. Fewer files, but it trips the lint rule, forces
every resolution test through `render()`, and gives the duplication comment
nothing precise to name.

---

## 3. State model

```ts
type ThemePreference = "light" | "dark" | "system";
type ResolvedTheme = "light" | "dark";
```

The provider holds exactly two pieces of state:

```ts
const [preference, setPreferenceState] = useState<ThemePreference>(readStoredPreference);
const [systemIsDark, setSystemIsDark] = useState<boolean>(prefersDark);
const resolved: ResolvedTheme = resolveTheme(preference, systemIsDark);
```

`resolved` is **derived during render**, never stored. That single choice
discharges several requirements without any code of their own:

- FR-5.2 (OS changes are inert under Light/Dark) falls out of `resolveTheme`
  ignoring `systemIsDark` unless `preference === "system"`.
- Switching back to System cannot show a stale value, because there is no cached
  value to be stale.
- The provider cannot enter a state where `preference` and `resolved` disagree.

Both `useState` calls use lazy initializers, so storage and `matchMedia` are read
once at mount rather than on every render.

**Rejected alternative — store `resolved` and subscribe only while on System.**
This is the more common shape in the wild and it is what FR-5.1/5.3 literally
describe. It was rejected because it adds a second state variable that must be
kept consistent with the first, and re-registers the listener on every preference
change. The always-on listener costs one `MediaQueryList` for the life of the
app. FR-5.3's requirement (registered and torn down through effect cleanup, no
leaks) is still met — the effect just has a stable, empty dependency list.

**Rejected alternative — `useSyncExternalStore` over the media query.** It is the
textbook-correct primitive for an external mutable source, but the app never
server-renders and never reads the value during a concurrent transition, so the
tearing it prevents cannot occur here. It buys no correctness and costs a
subscribe/getSnapshot indirection.

---

## 4. Applying the theme to the DOM

`applyTheme` is the only runtime writer:

```ts
export function applyTheme(resolved: ResolvedTheme): void {
  const root = document.documentElement;
  root.classList.toggle("dark", resolved === "dark");
  root.style.colorScheme = resolved;
}
```

`classList.toggle` touches exactly one token, satisfying FR-2.4 by construction —
there is no `className = ...` assignment anywhere that could clobber a sibling
class. The function is idempotent, which is what makes FR-4.3 work: the provider
re-applying what the boot script already wrote is a no-op, not a
clear-then-restore flicker.

It is called from a **`useLayoutEffect`** keyed on `resolved`, not `useEffect`.
Layout effects run before the browser paints, so a preference change is committed
in the same frame as the click (FR-2.3), and mounting cannot produce a painted
frame in the wrong theme.

### `color-scheme` in two places, deliberately

`src/index.css` currently declares `:root { color-scheme: light dark; }`, which
tells the browser the page handles both — true of the tokens, false of the app.
It becomes:

```css
:root { color-scheme: light; }
.dark { color-scheme: dark; }
```

That is the no-JS floor: correct native scrollbars and form controls from the
stylesheet alone. `applyTheme` *also* writes `root.style.colorScheme`, which wins
on specificity and, more importantly, is in effect during the window before the
stylesheet has been parsed — the exact window the boot script exists to cover.
Both writers always agree, because both are driven by the same resolved value.

---

## 5. Pre-paint boot script

A classic (non-`module`, non-`defer`) inline script, first child of `<head>`:

```html
<script>
  // Pre-paint theme application. This is a hand-inlined copy of
  // resolveTheme + applyTheme from src/lib/theme/apply.ts and the storage key
  // from src/lib/theme/types.ts. A module script cannot run before first paint,
  // so this cannot import them. Change one, change the other.
  (function () {
    try {
      var pref = localStorage.getItem("converge.theme");
      if (pref !== "light" && pref !== "dark" && pref !== "system") pref = "system";
      var dark =
        pref === "dark" ||
        (pref === "system" &&
          typeof window.matchMedia === "function" &&
          window.matchMedia("(prefers-color-scheme: dark)").matches);
      document.documentElement.classList.toggle("dark", dark);
      document.documentElement.style.colorScheme = dark ? "dark" : "light";
    } catch {
      // Storage or matchMedia unavailable: leave the light default in place.
    }
  })();
</script>
```

Notes that matter at implementation time:

- It must not be `type="module"` — module scripts are deferred and always run
  after first paint, which defeats the entire purpose.
- Vite leaves inline classic scripts in `index.html` untouched through the build;
  no plugin or transform is needed.
- The allowlist check happens *before* anything derived from the stored value
  reaches the DOM, so a hand-tampered `converge.theme` can never become an
  arbitrary class name or `color-scheme` value (PRD §8, Security).
- `index.html` is **not** in `.prettierignore`, so the script must be written in
  Prettier's formatting or `npm run format:check` will fail. Run `npm run format`
  after editing.

The counterpart comment in `apply.ts` names `index.html`, closing FR-4.4 from
both directions.

**Rejected alternative — skip the script and accept one flash.** The PRD makes
"no white flash" an acceptance criterion, and the flash is worst on exactly the
machine most likely to have Dark stored.

---

## 6. Provider placement and the picker

### 6.1 `App.tsx`

```tsx
<QueryClientProvider client={queryClient}>
  <ThemeProvider>
    <BrowserRouter>
      <AppShell>
        <AppRoutes />
      </AppShell>
      <ThemedToaster />
    </BrowserRouter>
  </ThemeProvider>
</QueryClientProvider>
```

`ThemedToaster` is a one-line local component that reads `useTheme()` and renders
`<Toaster richColors position="top-right" theme={resolved} />`. It exists because
the `Toaster` must be *inside* the provider to consume context (FR-8.1), and
because isolating the read keeps a theme change from being the reason `App`
re-renders.

`ThemeProvider` sits **inside** `QueryClientProvider`. The `queryClient` is a
module-level singleton, so its cache is immune to re-renders either way — but
keeping the provider below it makes the dependency direction obvious and matches
the existing nesting order. A theme change re-renders the subtree below
`ThemeProvider`; that subtree is the UI, which must re-render anyway to pick up
the new `resolved` value. No query is invalidated and no fetch is triggered,
satisfying the PRD's performance requirement.

The context value is `useMemo`'d over `[preference, resolved, setPreference]`,
and `setPreference` is a `useCallback` with an empty dependency list, so a parent
re-render does not cascade a new object through every consumer.

### 6.2 The shell

`AppShell` is a plain wrapper component, not a React Router layout route:

```tsx
<div className="flex min-h-full flex-col">
  <header className="sticky top-0 z-40 flex h-14 items-center justify-between
                     border-b border-border bg-background px-4">
    <Link to="/" className="text-sm font-semibold text-foreground">Converge</Link>
    <ThemeToggle />
  </header>
  <main className="flex-1">{children}</main>
</div>
```

**Why a wrapper rather than a layout route with `<Outlet />`.** The shell applies
unconditionally to every path, including the `*` not-found branch, and needs no
route data. Wrapping in `App.tsx` leaves `routes.tsx` a pure route table and
leaves `src/__tests__/routes.test.tsx` structurally untouched — the strongest
possible reading of FR-7.3. The layout-route form is the right refactor the day a
route needs to opt *out* of the shell; nothing does today, so YAGNI.

FR-7.5 (do not crowd the review page): the header is `sticky`, not `fixed`, so it
occupies layout space instead of overlaying the diff, and it declares no
`max-w-*` of its own. Every page keeps its existing `mx-auto max-w-*` container,
including `ReviewPage`'s `max-w-7xl`. `AppShell` does not render a `PageHeader`
and owns no title, so FR-7.4's separation holds.

**Open question 2 — resolved.** The wordmark is a `<Link to="/">`. The review
flow is a three-step wizard, "start over" is a real need mid-flow, and a
clickable home affordance is the conventional expectation for a wordmark. It
costs one import.

### 6.3 The picker

`ThemeToggle` uses `DropdownMenuRadioGroup` / `DropdownMenuRadioItem`, not plain
menu items:

```tsx
<DropdownMenu>
  <DropdownMenuTrigger asChild>
    <Button variant="ghost" size="icon" aria-label="Change theme">
      {resolved === "dark" ? <Moon /> : <Sun />}
    </Button>
  </DropdownMenuTrigger>
  <DropdownMenuContent align="end">
    <DropdownMenuRadioGroup value={preference} onValueChange={...}>
      <DropdownMenuRadioItem value="light"><Sun /> Light</DropdownMenuRadioItem>
      <DropdownMenuRadioItem value="dark"><Moon /> Dark</DropdownMenuRadioItem>
      <DropdownMenuRadioItem value="system"><Monitor /> System</DropdownMenuRadioItem>
    </DropdownMenuRadioGroup>
  </DropdownMenuContent>
</DropdownMenu>
```

The radio group is the load-bearing choice. Exactly one of three is selected, so
Radix emits `role="menuitemradio"` with `aria-checked` — which means FR-6.2
("the current preference is visibly marked") is announced to screen readers, not
just drawn, and is assertable in a test as
`getByRole("menuitemradio", { name: /system/i, checked: true })` rather than by
sniffing for a check-icon element. Keyboard operation (FR-6.4) comes from Radix.

The trigger icon reflects **`resolved`** (FR-6.5 — what the app looks like right
now); the checked item reflects **`preference`** (FR-6.2 — what the user chose).
These deliberately differ when System is selected, which is the whole point of
FR-1.5.

`size="icon"` (`size-8`) and `variant="ghost"` both exist in the vendored
`button.tsx`.

### 6.4 Vendoring `dropdown-menu`

`npx shadcn@latest add dropdown-menu` writes `src/components/ui/dropdown-menu.tsx`
in the `radix-nova` style from `components.json`. No new dependency is needed:
`radix-ui@1.6.7` already re-exports the primitive
(`node_modules/radix-ui/dist/index.d.ts:21-22`), which is the same package
`select.tsx` imports from. `src/components/ui/**` is ignored by both
`eslint.config.js` and `.prettierignore`, so the generated file lands unmodified
and cannot fail lint or format checks.

If the registry is unreachable at implementation time, hand-write it mirroring
`select.tsx` — same `import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui"`
form, same `data-slot` attributes, same token classes (`bg-popover`,
`text-popover-foreground`, `focus:bg-accent`). Only Root, Trigger, Portal,
Content, RadioGroup, RadioItem, and ItemIndicator are used.

---

## 7. The diff view

`FileDiff` reads `useTheme()` and extends the options object it already builds:

```tsx
options={{
  diffStyle: "unified",
  expandUnchanged: true,
  collapsedContextThreshold: 8,
  overflow: "scroll",
  themeType: resolved,
  theme: { light: "pierre-light", dark: "pierre-dark" },
}}
```

**Open question 1 — resolved: use the package's own pair, not a Shiki theme.**
`@pierre/diffs@1.4.1` defines
`DEFAULT_THEMES = { dark: "pierre-dark", light: "pierre-light" }`
(`dist/constants.js:28-31`) and pre-registers those loaders from
`@pierre/theming`'s `pierreThemes` collection
(`dist/highlighter/shared_highlighter.js:66`). They are the pair the library's
own chrome — gutters, hunk separators, addition/deletion backgrounds — was
designed against, and `pierre-light` is what the review page renders today, so
adopting it changes nothing visually in light mode while getting a matched dark
counterpart for free. Passing them explicitly rather than relying on the default
satisfies FR-8.4 and makes the choice assertable in a test. The full 65-theme
Shiki catalog remains available through the same `theme` prop if a later task
wants to offer diff-theme selection; that is out of scope here.

**Open question 4 — resolved: no extra treatment needed.**
`dist/utils/cssWrappers.js:20-24` emits `:host { color-scheme: <themeType>; }`
into the diff's shadow root whenever `themeType !== "system"`. Passing the
resolved value — never the literal `"system"`, per FR-8.3 — is precisely what
gives the diff's internal scroll containers correct native scrollbars. This is
the second, independent reason FR-8.3 matters: `"system"` would both diverge from
an explicitly pinned app theme *and* suppress the shadow-root `color-scheme`
declaration entirely.

**Do not add `resolved` to the `key`.** `PatchDiff`'s `key={path}` stays as it
is. `dist/react/utils/useFileDiffInstance.js` compares incoming options with
`areOptionsEqual` — a *by-value* comparison that special-cases `theme`
(`dist/utils/areOptionsEqual.js`) — and calls `instance.setOptions(newOptions)`
with a forced re-render when they differ. So FR-8.5 (re-theme in place while the
review page is open) is satisfied by the existing prop flow, with no remount, no
re-parse of the patch, and no refetch. Keying on the theme would throw away the
parsed diff and the highlighter cache on every toggle.

One accepted rough edge: theme resolution inside `@pierre/diffs` is async (Shiki
loads the theme on demand), so switching modes with the review page open may show
the previous theme's styles for a frame or two before the new stylesheet is
attached. This is internal to the library, affects only the diff surface, and is
not worth working around.

---

## 8. Storage

```ts
export function readStoredPreference(): ThemePreference {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    return isThemePreference(raw) ? raw : DEFAULT_PREFERENCE;
  } catch {
    return DEFAULT_PREFERENCE;
  }
}

export function writeStoredPreference(preference: ThemePreference): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, preference);
  } catch {
    // Private browsing, disabled storage, quota: the session stays correct
    // in memory. Never let a failed write block applying the theme.
  }
}
```

Both are total functions — an absent key, a corrupt value, and a throwing store
all collapse to the same `"system"` default (FR-3.2, FR-3.3, FR-3.4). The write
is called from `setPreference` *after* the state update, never from an effect, so
a storage failure cannot be on the path that applies the theme.

`setPreference` persists the literal preference string, so selecting System
stores `"system"` and not the theme it happened to resolve to (FR-3.5).

No `storage`-event listener: cross-tab sync is not a requirement, and adding it
would make one tab's choice silently override another's.

---

## 9. Dark-mode correctness audit (§4.9)

The audit was run during design rather than deferred to implementation, because
its outcome determines how much work §4.9 actually is. Three greps over
`apps/frontend/src`:

| Check | Pattern | Result |
| --- | --- | --- |
| Literal Tailwind palette classes | `(bg\|text\|border\|ring\|fill\|stroke\|divide\|shadow)-(white\|black\|slate\|gray\|zinc\|neutral\|stone\|red\|orange\|amber\|yellow\|lime\|green\|emerald\|teal\|cyan\|sky\|blue\|indigo\|violet\|purple\|fuchsia\|pink\|rose)` | **0 matches** |
| Hex / `rgb()` / inline `style={{}}` | `#[0-9a-fA-F]{3,8}` , `rgba?\(` , `style=\{\{` | **0 matches in source**; the only hits are `#421`/`#435` PR numbers in test fixtures |
| Existing `dark:` overrides in app code | `dark:` under `components/common`, `components/features`, `pages` | **0** — all `dark:` usage lives in the vendored `components/ui/**` |

**Conclusion: FR-9.1 and FR-9.2 require no conversions.** Every component and
page already draws from semantic tokens — `bg-background`, `text-foreground`,
`text-muted-foreground`, `border-border`, `bg-muted`, `bg-destructive/5`,
`text-destructive`. The palette will simply start working the moment the `dark`
class appears. This table *is* the FR-9.4 record; implementation re-runs the same
three greps as a regression check and reports any new hit.

**FR-9.3 (semantic colors in dark mode).** The codebase has no green/amber status
palette — `ReviewStatus` is text plus `Skeleton`s, and `ChangeTable`/`Badge`
variants are all neutral tokens. The only meaning-bearing color is
`--destructive`, used by `ReviewErrorPanel` (`border-destructive/40`,
`bg-destructive/5`, `text-destructive`) and `Badge`'s `destructive` variant. The
tokens already flip: light `oklch(0.577 0.245 27.325)` on `oklch(1 0 0)`, dark
`oklch(0.704 0.191 22.216)` on `oklch(0.145 0 0)` — the dark variant is the
*lighter* red, which is the correct direction. `badge.tsx` additionally carries
`dark:bg-destructive/20`. Contrast is expected to clear AA comfortably;
implementation measures it rather than assuming.

**Open question 3 — resolved, and now nearly moot.** Since there are no
hardcoded colors to fix, the only way §4.9 can surface a problem is a token-value
problem. Per the PRD's own proposed resolution, such a finding is recorded as a
follow-up rather than acted on: repaletting is a §2 non-goal. The realistic
candidates are `--destructive` on dark and `--muted-foreground` (`oklch(0.708)`)
on `--muted` (`oklch(0.269)`); both are stock shadcn values.

---

## 10. Testing

### 10.1 `matchMedia` in jsdom

`vitest.config.ts` uses the `jsdom` environment, which does not implement
`matchMedia`; `src/test/setup.ts` currently does nothing but `cleanup()`. A new
`src/test/matchMedia.ts` installs a controllable stub and exposes
`setSystemDark(dark: boolean)`, which flips `matches` and dispatches `change` to
every registered listener — that is the lever the FR-5.1/5.2 tests pull.

`setup.ts` gains an `afterEach` that, alongside `cleanup()`, resets the stub to
light, clears `localStorage`, and restores `document.documentElement`'s
`className` and `style.colorScheme`. jsdom shares one document across a file, so
without this a test that goes dark leaks into the next one.

### 10.2 Provider in the render helper

`renderWithProviders` in `src/test/render.tsx` gains `ThemeProvider` inside its
existing `QueryClientProvider`/`MemoryRouter` nesting. `useTheme` throws outside
a provider — the honest contract, and the one that makes "the setter is the only
supported mutation path" (PRD §5) enforceable.

That contract has two consequences for existing tests, both anticipated by
FR-7.3's "updates confined to accommodating the new wrapper":

- `src/components/features/review/__tests__/FileDiff.test.tsx` calls bare
  `render(...)`; it moves to `renderWithProviders`.
- `src/__tests__/routes.test.tsx` builds its own provider tree by hand; it gains
  `ThemeProvider` in that tree.

No assertion in either file changes. All 97 existing tests must still pass.

### 10.3 New coverage

| Area | Assertions |
| --- | --- |
| `resolveTheme` | all three preferences × both OS states; `matchMedia` absent or throwing ⇒ `light` (FR-1.3, FR-1.4) |
| `storage` | round-trip; absent key ⇒ `system`; `"DARK"` / `"twilight"` / `""` ⇒ `system`; throwing `getItem`; throwing `setItem` does not prevent application (FR-3.1–FR-3.5) |
| `applyTheme` | `dark` class added/removed; `style.colorScheme` set; a pre-existing unrelated root class survives (FR-2.1, FR-2.2, FR-2.4) |
| `ThemeProvider` | boots from stored value; `setPreference` updates DOM and storage synchronously; `setPreference("system")` stores `"system"` (FR-2.3, FR-3.5) |
| OS tracking | `setSystemDark(true)` flips the theme under `system`, is inert under `light` and `dark` (FR-5.1, FR-5.2) |
| Listener cleanup | `removeEventListener` observed on unmount (FR-5.3) |
| `ThemeToggle` | three `menuitemradio` items; the current preference is `checked`; System stays checked while the trigger shows the resolved icon; selecting an item calls the setter and closes the menu (FR-6.1–FR-6.3, FR-6.5) |
| `AppShell` | renders the wordmark link, the theme control, and its children (FR-7.1, FR-7.2) |
| `FileDiff` | `vi.mock("@pierre/diffs/react")` captures `options`; asserts `themeType === "light"` / `"dark"` tracking the provider, and that it is never `"system"` (FR-8.2, FR-8.3) |

The `FileDiff` test keeps the existing file's standing note: assert on the props
Converge passes, never on `@pierre/diffs`' rendered output, which lives in a
shadow root and needs `ResizeObserver`.

Not covered by automated tests, verified by hand against the acceptance criteria:
the absence of a flash on load, and WCAG AA contrast measurements.

---

## 11. Requirement coverage

| Requirement | Where it is discharged |
| --- | --- |
| FR-1.1 – FR-1.5 | §3 — derived `resolved`, both values exposed |
| FR-2.1 – FR-2.4 | §4 — `applyTheme` via `useLayoutEffect`, `classList.toggle` |
| FR-3.1 – FR-3.5 | §8 — total read/write helpers |
| FR-4.1 – FR-4.4 | §5 — inline classic script, paired comments |
| FR-5.1 – FR-5.3 | §3 — one always-on listener, derivation handles the rest |
| FR-6.1 – FR-6.6 | §6.3, §6.4 — Radix radio group, vendored primitive |
| FR-7.1 – FR-7.5 | §6.2 — wrapper shell, sticky header, unchanged routes |
| FR-8.1 – FR-8.5 | §6.1, §7 — `ThemedToaster`, `themeType` + `theme`, no key change |
| FR-9.1 – FR-9.4 | §9 — audit run at design time; zero conversions needed |

---

## 12. Risks

- **shadcn registry unreachable.** Mitigation in §6.4: hand-write the primitive
  from `select.tsx`'s shape. No new dependency either way.
- **Prettier on `index.html`.** The file is not ignored; an unformatted inline
  script fails `npm run format:check`. Run `npm run format` after editing.
- **`StrictMode` double-invokes effects in development.** `applyTheme` is
  idempotent and the media-query effect has symmetric cleanup, so the double
  mount is harmless — but the listener-cleanup test must assert on
  `removeEventListener` calls rather than on a listener count.
- **A future component reintroduces a hardcoded color.** The §9 greps are cheap;
  re-running them is the regression check, and the reviewer can confirm coverage
  from the table rather than re-deriving it.

## 13. Out of scope

Unchanged from the PRD §2 non-goals: no backend or API change, no cross-device
sync, no palette redesign, no themes beyond light and dark, no per-page
overrides, and no theming of surfaces that do not yet exist. Diff-theme selection
(exposing the 65-theme Shiki catalog to the user) is explicitly deferred.
