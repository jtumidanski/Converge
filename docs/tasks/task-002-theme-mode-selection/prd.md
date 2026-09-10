# Light / Dark / System Mode Selection — Product Requirements Document

Version: v1
Status: Draft
Created: 2026-09-10

---

## 1. Overview

The Converge web UI already ships a complete dark palette. `apps/frontend/src/index.css`
defines a full `.dark` token block (background, foreground, card, popover, primary,
secondary, muted, accent, destructive, border, input, ring, chart-1..5, sidebar-\*)
alongside the `:root` light block, and declares `@custom-variant dark (&:is(.dark *))`
so Tailwind's `dark:` variant is wired to that class. What is missing is the one thing
that makes any of it observable: nothing ever puts the `dark` class on the document.
The app is permanently light, and `:root { color-scheme: light dark; }` currently lies
to the browser about a choice the user cannot actually make.

This task closes that gap. It introduces a theme system with three user-selectable
modes — **Light**, **Dark**, and **System** — where System defers to the operating
system's `prefers-color-scheme` and tracks changes to it live. The selection persists
across reloads in `localStorage` and is applied before first paint so there is no
white flash on a dark-mode machine. A dropdown control in a new persistent application
header exposes the choice.

Two categories of surface do not read our CSS variables and therefore need explicit
wiring rather than inheriting the theme for free: the `sonner` `Toaster` (which takes a
`theme` prop) and the `@pierre/diffs` `PatchDiff` component that renders every file
diff on the review page. Both are in scope. Finally, because a toggle that produces an
unreadable page is not a finished feature, this task includes an audit of the existing
components for hardcoded, light-only colors and converts them to semantic tokens.

The backend is not involved. Converge has no user accounts and persists only mirrors
and review sessions on the filesystem; a display preference is browser-local state.

## 2. Goals

Primary goals:

- Let a user choose Light, Dark, or System and have the entire UI honor that choice.
- Persist the choice across page reloads and browser restarts.
- Apply the resolved theme before first paint — no flash of the wrong theme.
- Track the OS preference live while in System mode, with no reload required.
- Ensure every existing screen is legible and visually correct in dark mode.
- Theme the diff view and toast notifications, not just the app chrome.

Non-goals:

- Any backend, API, or persistence-layer change. No server-side preference storage.
- Syncing the preference across browsers, devices, or profiles.
- Redesigning the light or dark palettes. The existing token values in `index.css`
  are taken as given; only *missing* or *hardcoded* colors are corrected.
- Additional themes beyond light and dark (high-contrast, custom accent colors,
  user-authored themes).
- Per-page or per-component theme overrides.
- Theming any surface that does not yet exist. Only the three current pages and the
  components they render are in scope.

## 3. User Stories

- As a developer reviewing code at night, I want to switch Converge to dark mode so
  that I can read diffs without eye strain.
- As a user whose OS is set to dark mode, I want Converge to start dark automatically
  so that I do not have to configure it.
- As a user whose OS switches to dark on a schedule, I want Converge to follow along
  mid-session so that the app matches the rest of my desktop without a reload.
- As a user who prefers light Converge on a dark desktop, I want to pin Light
  explicitly so that my OS setting does not override my per-app choice.
- As a returning user, I want my chosen mode to still be in effect when I reopen the
  app so that I only choose once.
- As a user loading the app in dark mode, I want no white flash during page load so
  that the experience is not jarring.
- As a keyboard or screen-reader user, I want to reach and operate the theme control
  without a mouse so that the feature is usable to me.

## 4. Functional Requirements

### 4.1 Theme state model

- **FR-1.1** The system defines exactly three user-selectable modes:
  `"light" | "dark" | "system"`. This is the *preference*.
- **FR-1.2** The system defines exactly two resolved themes: `"light" | "dark"`.
  This is what is actually applied to the DOM.
- **FR-1.3** Resolution rule: preference `light` resolves to `light`; preference
  `dark` resolves to `dark`; preference `system` resolves to `dark` when
  `window.matchMedia("(prefers-color-scheme: dark)").matches` is true, otherwise
  `light`.
- **FR-1.4** When `window.matchMedia` is unavailable or throws, preference `system`
  resolves to `light`. The app must not crash.
- **FR-1.5** Both the preference and the resolved theme are readable by consumers.
  A component that needs to know "am I currently dark?" reads the resolved theme; a
  component rendering the picker reads the preference (so `System` shows as selected
  rather than as whichever theme it resolved to).

### 4.2 Application to the DOM

- **FR-2.1** When the resolved theme is `dark`, the class `dark` is present on
  `document.documentElement`. When it is `light`, that class is absent.
- **FR-2.2** The `color-scheme` CSS property on the document root reflects the
  resolved theme (`light` or `dark`, not the dual `light dark` currently in
  `:root`), so that native form controls, scrollbars, and the canvas background
  match. The existing `:root { color-scheme: light dark; }` declaration is updated
  accordingly.
- **FR-2.3** Changing the preference updates the DOM synchronously on the same
  interaction — no perceptible delay, no reload.
- **FR-2.4** Applying the theme must not disturb any other class already present on
  `document.documentElement`.

### 4.3 Persistence

- **FR-3.1** The preference is stored in `localStorage` under the key
  `converge.theme`, with the literal values `light`, `dark`, or `system`.
- **FR-3.2** On load, an absent key resolves to the default preference `system`.
- **FR-3.3** On load, a present but unrecognized value (corrupt, truncated, or written
  by an older version) is treated as absent and resolves to `system`. It must not
  throw, and it must not leave the app unthemed.
- **FR-3.4** All `localStorage` access is wrapped so that a throwing store — private
  browsing, disabled storage, quota exhaustion — degrades to in-memory-only behavior
  for the session rather than breaking the app. A failure to persist must never
  prevent the theme from being applied.
- **FR-3.5** Selecting `system` persists the literal string `system`, not the theme
  it currently resolves to. A user on System must stay on System after a reload even
  if their OS preference changed in between.

### 4.4 Flash prevention

- **FR-4.1** An inline, render-blocking script in `apps/frontend/index.html`, placed
  in `<head>` before any stylesheet or module script, reads the stored preference,
  resolves it, and sets the `dark` class and `color-scheme` on
  `document.documentElement` before the first paint.
- **FR-4.2** That script is self-contained, wrapped in try/catch, and must never
  throw in a way that blocks the app from booting. On any error it leaves the
  document in the light default.
- **FR-4.3** The React provider must adopt the state the inline script established
  without producing a visible flicker on hydration — mounting must not transiently
  clear and re-apply the class.
- **FR-4.4** The storage key and the resolution rule are duplicated between the inline
  script and the TypeScript module by necessity (the script cannot import). The
  duplication is documented with a comment at both sites naming the other, so the two
  cannot silently drift.

### 4.5 System-preference tracking

- **FR-5.1** While the preference is `system`, a `change` listener on the
  `(prefers-color-scheme: dark)` media query updates the resolved theme live.
- **FR-5.2** While the preference is `light` or `dark`, OS changes have no effect.
- **FR-5.3** The listener is registered and torn down through React effect cleanup;
  no listener leaks across provider unmounts or preference changes.

### 4.6 The theme control

- **FR-6.1** The control is an icon-trigger dropdown menu offering exactly three
  items — Light, Dark, System — each with a text label and a Lucide icon
  (sun / moon / monitor).
- **FR-6.2** The item matching the current *preference* is visibly marked as selected.
- **FR-6.3** Selecting an item sets that preference, applies it, persists it, and
  closes the menu.
- **FR-6.4** The trigger has an accessible name (e.g. `aria-label="Change theme"`),
  is reachable by keyboard, and the menu is operable with arrow keys, Enter, and
  Escape. This follows from using the shadcn/Radix `dropdown-menu` primitive rather
  than a hand-rolled menu.
- **FR-6.5** The trigger icon reflects the current resolved theme, so the button
  communicates state at a glance without opening the menu.
- **FR-6.6** The `dropdown-menu` shadcn component does not yet exist under
  `src/components/ui/`; it is added following the existing pattern of the components
  already vendored there (`select.tsx`, `collapsible.tsx`), using the project's
  `radix-nova` style from `components.json`.

### 4.7 Application shell

- **FR-7.1** A new layout component renders a persistent top bar containing the
  Converge wordmark and the theme control, with page content beneath it.
- **FR-7.2** All three existing routes (`SelectRepositoryPage`,
  `SelectChangesPage`, `ReviewPage`) render inside this shell, so the control is
  reachable from every screen.
- **FR-7.3** The shell is introduced without changing the URL structure or the
  existing route definitions' paths. Existing route tests must continue to pass, with
  updates confined to accommodating the new wrapper.
- **FR-7.4** The shell does not duplicate `PageHeader`. Pages keep rendering their own
  `PageHeader` for title, description, and page-specific actions; the shell sits above
  it and owns only app-global chrome.
- **FR-7.5** On the review page, which is the densest screen, the shell must not
  obscure or crowd the diff view.

### 4.8 Non-CSS-variable surfaces

- **FR-8.1** The `sonner` `Toaster` in `App.tsx` receives a `theme` prop driven by the
  resolved theme, so toasts are not light-on-light or dark-on-dark.
- **FR-8.2** `FileDiff` passes theme information to `@pierre/diffs` `PatchDiff` through
  the `options` prop it already constructs. Verified against the installed package's
  type definitions (`@pierre/diffs@1.4.1`, `dist/types.d.ts`):
  - `BaseCodeOptions.themeType?: 'system' | 'light' | 'dark'`
  - `BaseCodeOptions.theme?: DiffsThemeNames | Record<'light' | 'dark', DiffsThemeNames>`
    where `DiffsThemeNames` is a Shiki `BundledTheme` name or an arbitrary string.
- **FR-8.3** `themeType` is set from the **resolved** theme (`light` or `dark`), never
  the literal `system`. Passing `system` would make the diff follow the OS directly and
  diverge from the app whenever the user has explicitly pinned Light or Dark.
- **FR-8.4** A concrete light theme name and dark theme name are chosen from Shiki's
  bundled set and supplied via `theme`, so the diff's syntax highlighting is legible in
  both modes. The specific pair is a design-phase decision.
- **FR-8.5** Changing the theme while the review page is open must re-render the diff
  in the new theme without requiring navigation away and back.

### 4.9 Dark-mode correctness audit

- **FR-9.1** Every component under `src/components/` and `src/pages/` is audited for
  color utilities that are not semantic tokens — literal Tailwind color classes
  (`bg-white`, `text-gray-*`, `border-slate-*`, and similar), inline `style` colors,
  and hex or `rgb()` literals.
- **FR-9.2** Each such occurrence is either converted to the appropriate semantic token
  (`bg-background`, `text-muted-foreground`, `border-border`, …) or, where a literal
  color is genuinely intended, given an explicit `dark:` counterpart.
- **FR-9.3** Status and severity colors — the `Badge` variants, `ReviewStatus`, and
  `ReviewErrorPanel`, where red/green/amber carry meaning — must retain sufficient
  contrast in dark mode rather than being flattened to neutral tokens.
- **FR-9.4** The audit findings and their resolutions are recorded, so a reviewer can
  confirm coverage rather than re-deriving it.

## 5. API Surface

**No HTTP API changes.** This feature adds no endpoints, modifies no request or
response shapes, and introduces no new error cases. The backend Go module is untouched.

The only build-level interaction with the backend is the existing one: `npm run build`
in `apps/frontend` emits into `apps/backend/internal/ui/dist`, which the `converge`
binary embeds. That pipeline is unchanged.

The feature's internal surface is a React context module. Its exact naming and file
layout are a design-phase decision; the contract it must satisfy is:

- A provider component that owns the preference, resolves it, applies it to the DOM,
  persists it, and subscribes to the OS media query.
- A consumer hook exposing the current preference, the resolved theme, and a setter
  that accepts one of the three modes.
- The setter is the only supported way to change the theme; nothing else writes the
  `dark` class or the storage key at runtime (the inline boot script in `index.html`
  is the one deliberate exception, and runs before the provider exists).

## 6. Data Model

No server-side entities, no `session.json` fields, no filesystem layout changes, and
no migrations.

The single piece of new persistent state is browser-local:

| Store          | Key              | Values                        | Default    |
| -------------- | ---------------- | ----------------------------- | ---------- |
| `localStorage` | `converge.theme` | `"light" \| "dark" \| "system"` | `"system"` |

Compatibility: no prior version wrote this key, so there is no legacy data to migrate.
Forward compatibility is handled by FR-3.3 — any unrecognized value is treated as
absent, which means a future version that adds a fourth mode will degrade gracefully
on an older build rather than breaking it.

## 7. Service Impact

**`apps/frontend`** — all substantive changes:

- `index.html` — inline pre-paint theme script (FR-4.1).
- `src/index.css` — `color-scheme` handling per FR-2.2; any token gaps found by the
  §4.9 audit.
- `src/App.tsx` — wrap the tree in the theme provider; drive `Toaster`'s `theme` prop.
- New theme provider/hook module and its tests.
- New application shell layout component and its tests.
- New `src/components/ui/dropdown-menu.tsx` (vendored shadcn primitive).
- New theme-picker component and its tests.
- `src/components/features/review/FileDiff.tsx` — pass `themeType` and `theme` through
  the existing `options` prop.
- Existing components and pages — semantic-token corrections from the §4.9 audit.
- `src/routes.tsx` and `src/__tests__/routes.test.tsx` — accommodate the shell.

**`apps/backend`** — no source changes. The embedded `internal/ui/dist` bundle is
regenerated by the frontend build, so `make build` and `make docker-build` must still
pass, but no Go file is edited.

## 8. Non-Functional Requirements

**Performance**

- The inline boot script is render-blocking by design and must stay tiny — a single
  storage read, a media query check, and two DOM writes. No dependencies, no parsing
  beyond a string comparison.
- Toggling the theme must not remount the route tree or refetch data. Theme context
  changes must not invalidate React Query caches or cause the review page to re-request
  diffs.
- The theme context value must be memoized so that a re-render of the provider's
  parent does not cascade a re-render through every consumer.

**Accessibility**

- The control satisfies FR-6.4 (accessible name, keyboard operation, focus management)
  by using the Radix-backed shadcn primitive rather than a bespoke menu.
- Text and interactive elements meet WCAG AA contrast (4.5:1 for body text, 3:1 for
  large text and UI boundaries) in both themes. This is the measurable bar for the
  §4.9 audit, particularly FR-9.3's semantic colors.
- The feature respects that some users pin a theme deliberately; System is the default
  but never overrides an explicit choice.

**Security**

- The inline script in `index.html` is static, author-authored content with no
  interpolation of any runtime or user-supplied value. It reads `localStorage` and
  writes a class name from a closed set of literals — no `innerHTML`, no `eval`, no
  dynamic code construction.
- The value read from `localStorage` is validated against the three-value allowlist
  before use (FR-3.3), so a manually tampered storage value cannot reach the DOM as an
  arbitrary class name or `color-scheme` value.
- No credential, token, repository path, or provider data is stored, read, or logged by
  this feature.

**Observability**

- No new logging, metrics, or telemetry. The feature is client-only and its state is
  directly inspectable in the DOM and in `localStorage`.

**Testing**

Vitest, matching the project's existing patterns (see `src/test/render.tsx`):

- Resolution logic for all three preferences against both OS states, including the
  `matchMedia`-unavailable fallback (FR-1.4).
- Persistence: read, write, absent key, corrupt value, and a throwing `localStorage`
  (FR-3.1 – FR-3.5).
- DOM application: `dark` class and `color-scheme` present/absent as expected, and
  unrelated root classes preserved (FR-2.1, FR-2.2, FR-2.4).
- Live OS tracking: simulated media-query `change` updates the theme under `system`
  and is ignored under `light`/`dark` (FR-5.1, FR-5.2).
- Listener cleanup on unmount (FR-5.3).
- The picker: renders three options, marks the current preference, and invokes the
  setter on selection (FR-6.1 – FR-6.3).
- The shell renders the control and its children (FR-7.1, FR-7.2).
- `FileDiff` forwards the resolved theme into `PatchDiff`'s options (FR-8.2, FR-8.3).
  Consistent with the existing note in `FileDiff.test.tsx`, assert on the props Converge
  passes, not on `@pierre/diffs`' internal rendered output.

Note that `jsdom` does not implement `matchMedia`; the test setup will need to provide
it. Check `src/test/setup.ts` before assuming it is absent.

## 9. Open Questions

1. **Shiki theme pair for the diff view (FR-8.4).** Which bundled light and dark theme
   names best match the neutral `radix-nova` palette? Design phase should compare
   candidates against the actual review page rather than choosing from reputation.
2. **Wordmark in the shell (FR-7.1).** Is the header a plain text wordmark, or should
   it be a link back to the repository-selection route? A clickable home affordance is
   a reasonable default but was not part of the original ask.
3. **Scope of the §4.9 audit's outcome.** If the audit surfaces contrast problems that
   stem from the *token values themselves* rather than from hardcoded colors, fixing
   them would mean editing the palette — which §2 lists as a non-goal. The proposed
   resolution: fix hardcoded colors in this task, and record any token-value problems
   as findings for a follow-up rather than silently expanding scope. Confirm during
   design.
4. **`color-scheme` and the diff view.** Whether `@pierre/diffs`' internal scroll
   containers pick up the root `color-scheme` for their scrollbars, or need their own
   treatment. Verify empirically during implementation.

## 10. Acceptance Criteria

Functional:

- [ ] A theme control is visible and operable on all three routes.
- [ ] Selecting **Dark** turns the entire UI dark immediately, with no reload.
- [ ] Selecting **Light** turns it light immediately, even when the OS is dark.
- [ ] Selecting **System** matches the current OS preference immediately.
- [ ] With **System** selected, changing the OS preference updates the UI live,
      with no reload and no interaction.
- [ ] With **Light** or **Dark** selected, changing the OS preference does nothing.
- [ ] The selection survives a full page reload and a browser restart.
- [ ] Loading the app with **Dark** stored produces no white flash at any point.
- [ ] A hand-corrupted `converge.theme` value loads cleanly as System.
- [ ] With `localStorage` unavailable, the app still themes correctly for the session.
- [ ] The control marks the current preference, and shows **System** as selected —
      not the theme System resolved to.
- [ ] The trigger is keyboard-reachable; the menu opens, navigates, selects, and
      dismisses via keyboard alone.

Visual correctness:

- [ ] All three pages are fully legible in dark mode, with no light-on-light or
      dark-on-dark text, and no stray white panels.
- [ ] Diffs on the review page render in a theme matching the app, with additions,
      deletions, and syntax highlighting all legible.
- [ ] Changing the theme with the review page open re-themes the visible diff in place.
- [ ] Toasts (`sonner`) match the active theme in both modes.
- [ ] Status and severity colors remain distinguishable and meet contrast requirements
      in dark mode.
- [ ] No component contains a hardcoded light-only color without a `dark:` counterpart.

Verification — every command clean, per `CLAUDE.md`:

- [ ] `npm run lint` (cwd `apps/frontend`)
- [ ] `npm run format:check`
- [ ] `npm test` — the 97 pre-existing tests still pass, plus new coverage for §8
- [ ] `npm run build`
- [ ] `make lint`, `make test`, `make test-integration`, `make build`,
      `make docker-build` from the repository root
- [ ] Code review completed via `/audit-plan` or
      `superpowers:requesting-code-review` before the PR is opened
