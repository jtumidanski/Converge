# Dark-mode audit regression check (Task 10)

Date: 2026-09-10 (UTC)

This re-runs the §4.9 dark-mode audit from `design.md` as a regression check
against the code added by Tasks 1–9 of this plan (theme types/utilities,
`ThemeProvider`, the boot script, `ThemeToggle`, `AppShell` wiring,
`ThemedToaster`, and the `FileDiff` theme options). All commands were run
from `apps/frontend`.

## Command 1 — hardcoded Tailwind palette classes

```bash
grep -rnE '(bg|text|border|ring|fill|stroke|divide|shadow)-(white|black|slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)' \
  src --include='*.tsx' --include='*.ts' | grep -v '^src/components/ui/'
```

**Output:** (no matches — exit code 1)

**Result: no findings.** No component outside `src/components/ui/**` uses a
hardcoded Tailwind palette class. Every component still draws from semantic
tokens (`bg-background`, `text-muted-foreground`, `border-border`, etc.).

## Command 2 — hex/rgb literals and inline `style` colors

```bash
grep -rnE '#[0-9a-fA-F]{3,8}|rgba?\(|style=\{\{' src --include='*.tsx' --include='*.ts' \
  | grep -v '^src/components/ui/'
```

**Output:**

```
src/components/features/review/__tests__/ReviewErrorPanel.test.tsx:19:      baseDescription: "Immediately before #421",
src/components/features/review/__tests__/ReviewErrorPanel.test.tsx:26:          "#435 conflicts while being applied. It may depend on work that is not part of this review.",
src/components/features/review/__tests__/ReviewErrorPanel.test.tsx:49:    expect(screen.getByText(/#435 conflicts/i)).toBeInTheDocument();
src/components/features/review/__tests__/ReviewErrorPanel.test.tsx:51:    expect(screen.getByText(/#421/)).toBeInTheDocument();
src/components/features/review/__tests__/ReviewStatus.test.tsx:11:      ["applying:435", /applying #435/i],
src/lib/hooks/api/__tests__/useReviews.test.tsx:32:    baseDescription: "Immediately before #421",
src/pages/__tests__/ReviewPage.test.tsx:38:    baseDescription: "Immediately before #421",
src/pages/__tests__/ReviewPage.test.tsx:101:    expect(await screen.findByText(/applying #421/i)).toBeInTheDocument();
src/pages/__tests__/ReviewPage.test.tsx:103:      await screen.findByText(/Immediately before #421/, {}, { timeout: 6000 }),
src/pages/__tests__/ReviewPage.test.tsx:107:    expect(screen.getByRole("link", { name: /#421/ })).toHaveAttribute(
src/pages/__tests__/ReviewPage.test.tsx:194:          message: "#421 is not merged yet. Converge can only reconstruct merged PRs/MRs.",
src/pages/__tests__/ReviewPage.test.tsx:281:          message: "#435 conflicts while being applied.",
src/pages/__tests__/ReviewPage.test.tsx:290:    expect(await screen.findByText(/#435 conflicts/i)).toBeInTheDocument();
```

**Result: no findings.** Every hit is the `#421` / `#435` PR/MR numbers used
in test fixtures, exactly as the design's audit predicted. No hex color
literal, `rgb()`/`rgba()` call, or inline `style={{ ... }}` color exists
outside `src/components/ui/**`.

## Command 3 — `dark:` Tailwind variant usage outside `src/components/ui/**`

```bash
grep -rn 'dark:' src/components/common src/components/features src/components/layout \
  src/components/theme src/pages
```

**Output:**

```
src/components/features/review/FileDiff.tsx:42:          theme: { light: "pierre-light", dark: "pierre-dark" },
src/components/features/review/__tests__/FileDiff.test.tsx:82:    expect(lastOptions().theme).toEqual({ light: "pierre-light", dark: "pierre-dark" });
```

**Result: no findings — false-positive substring match.** Both hits are the
literal string `dark:` inside a JavaScript object key (`{ light: "...",
dark: "pierre-dark" }`), which is the Shiki dual-theme option object passed
to `shikiToDom`/the highlighter (added in Task 9, FR-8.2–FR-8.5). This is
not a Tailwind `dark:` variant class and is not a hardcoded palette
reference — it names the two Shiki syntax-highlighting theme identifiers
(`pierre-light`, `pierre-dark`) the diff viewer already switches between
based on the active theme. No conversion applies here.

## Summary

All three greps are clean against the intent of the audit: zero hardcoded
palette classes, zero hex/`rgb()` literals or inline `style` colors, and
zero real `dark:` Tailwind-variant usage outside the lint-ignored
`src/components/ui/**`, across all code this plan added. No fixes were
required (FR-9.1, FR-9.2, FR-9.3 hold).

## Follow-up candidates carried from the design (out of scope — not fixed)

Repaletting is a stated non-goal of this plan. These two token-value
observations from the design's §4.9 audit are noted here for a future task,
not addressed by Task 10:

- `--destructive` on dark (`oklch(0.704 0.191 22.216)`) rendered on
  `oklch(0.145 0 0)` (dark background) — **now measured, see §Contrast
  measurements below: passes AA.**
- `--muted-foreground` (`oklch(0.708 0 0)`) on `--muted`
  (`oklch(0.269 0 0)`) — **now measured, see below: passes AA.**

## Contrast measurements (computed from the `oklch()` token definitions)

These ratios were **computed, not browser-measured**: each `oklch()` value in
`apps/frontend/src/index.css` was converted to sRGB, Tailwind's `/N` opacity
was composited over the surface behind it, and the WCAG 2.x relative-luminance
contrast formula applied. They are token-level figures; item 9 of the manual
checklist below remains open for a real in-browser confirmation.

### Dark mode — all pairs pass WCAG AA (≥ 4.5:1)

| Pair | Foreground | Background | Ratio |
|---|---|---|---|
| Destructive `Badge` | `--destructive` `oklch(0.704 0.191 22.216)` | `dark:bg-destructive/20` over `--card` `oklch(0.205 0 0)` | **4.63:1** |
| `ReviewErrorPanel` body text | `--foreground` `oklch(0.985 0 0)` | `bg-destructive/5` over `--background` `oklch(0.145 0 0)` | **18.17:1** |
| Muted text on muted surface | `--muted-foreground` `oklch(0.708 0 0)` | `--muted` `oklch(0.269 0 0)` | **5.83:1** |

The destructive `Badge` figure uses `--card` as the surface behind the 20%
destructive wash, which is the lower of the two realistic placements; over
`--background` the same pair measures 5.30:1. Both pass.

### Pre-existing, out of scope: two light-mode pairs below 4.5:1

The same computation found two pairs under AA. Both are **light mode**, both
come from token values that predate this branch, and neither is touched by
this task (repaletting is a stated non-goal). Recorded for a future task; **not
caused by, and not fixed by, this branch.**

| Pair | Ratio |
|---|---|
| Light destructive `Badge`: `--destructive` `oklch(0.577 0.245 27.325)` on `bg-destructive/10` over `--background` | 3.99:1 |
| Light `--muted-foreground` `oklch(0.556 0 0)` on `--muted` `oklch(0.97 0 0)` | 4.34:1 |

## CI result for this branch

All repository-root and frontend gates were run and are green:

| Gate | Result |
|---|---|
| `make lint` | pass |
| `make test` | pass — 193/193 frontend tests (163 before the boot-script behavioral and `ThemeToggle` label tests were added), 18 backend packages |
| `make test-integration` | pass |
| `make build` | pass |
| `make docker-build` | pass |
| `npm run lint` (`apps/frontend`) | pass |
| `npm run format:check` (`apps/frontend`) | pass |
| `npm test` (`apps/frontend`) | pass |
| `npm run build` (`apps/frontend`) | pass |

This records the automated gates only. It does not discharge any item in the
manual checklist below.

## Step 3 — manual verification (NOT performed by the agent)

This environment has no browser and no OS theme control, so none of the
following could be verified by the agent. These are explicit checklist
items for a human to run through before merging. Steps to reproduce are
included so they can be run directly.

1. **Immediate effect, no reload.** Run `npm run dev`, open each of the
   three routes (repository picker / home, the review page, and any other
   top-level route), open the `ThemeToggle` in `AppShell`, and select Dark,
   then Light, then System in turn on each route. Confirm the whole page
   re-themes instantly with no reload and no flash.
2. **Live OS-theme following under System.** With System selected, flip the
   OS/browser dark-mode setting (e.g. via OS Settings, or DevTools'
   "Emulate CSS media feature prefers-color-scheme") and confirm the UI
   updates live. Re-pin to Light or Dark and flip the OS setting again;
   confirm the UI does *not* change while pinned.
3. **Persistence across reload and browser restart.** Pick a theme, reload
   the page, and confirm it is retained. Fully quit and reopen the browser
   and confirm it is still retained (checks `localStorage["converge.theme"]`
   survives a full restart, not just a soft reload).
4. **No white flash on hard reload with Dark stored.** With
   `localStorage["converge.theme"] = "dark"`, do a hard reload (disable
   cache in DevTools, or Ctrl/Cmd+Shift+R) and visually confirm no white
   flash appears at any point before first paint. If it flashes, check
   `index.html`: the inline boot script must still be the first child of
   `<head>` with no `type`, `defer`, or `async` attribute.
5. **Invalid stored value falls back to System.** Run
   `localStorage["converge.theme"] = "twilight"` in DevTools console, then
   reload. Confirm the page loads cleanly (no console error visible to the
   user) and resolves as if System were selected (i.e. follows the OS
   preference).
6. **Legibility on all three pages in dark mode.** With Dark selected,
   visually walk all three routes and confirm: no light-on-light or
   dark-on-dark text anywhere, and no stray white panels/cards that didn't
   pick up the dark background.
7. **Diff viewer re-themes in place.** Open the review page with a file
   diff visible, confirm it renders in `pierre-dark` under Dark and
   `pierre-light` under Light, with additions, deletions, and syntax
   highlighting all legible in both. Toggle the theme with the page open
   and confirm the diff re-themes without navigating away (a frame or two
   of the old theme during the switch is expected, per Shiki's on-demand
   theme loading).
8. **Toast matches active theme.** Force an API error (e.g. stop the
   backend, or trigger a known error path) to surface a toast, and confirm
   its colors match the active theme in both Light and Dark.
9. **`ReviewErrorPanel` / destructive `Badge` contrast ≥ 4.5:1 in dark
   mode.** Using browser DevTools' contrast checker (or the accessibility
   inspector) on the rendered destructive text/background pair in dark
   mode, confirm the rendered pairs against the computed figures in
   §"Contrast measurements" above:
   - `ReviewErrorPanel` text vs. its background: computed **18.17:1**
     (passes AA) — **browser confirmation still open**
   - Destructive `Badge` text vs. its background: computed **4.63:1**
     (passes AA) — **browser confirmation still open**

   These two spots are exactly where the follow-up token-value
   observations above (`--destructive` and `--muted-foreground`) would
   surface as a real contrast failure, so this check matters even though
   the greps found nothing and the computed ratios pass.

None of the Step 3 items were fabricated as passing; they are recorded here
as open checklist items pending a human running the app in an actual
browser.
