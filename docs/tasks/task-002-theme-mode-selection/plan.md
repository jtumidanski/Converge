# Light / Dark / System Mode Selection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Converge web UI a Light / Dark / System theme selector that persists, applies before first paint, tracks the OS preference live, and themes every surface including the diff view and toasts.

**Architecture:** Four layers. A pure core under `src/lib/theme/` (types, storage, resolution, one DOM writer) with no React. A `ThemeProvider` that holds the preference plus the OS bit, derives the resolved theme during render, and applies it in a layout effect. A hand-inlined classic `<script>` in `index.html` that writes the same class before first paint. Consumers: a Radix radio-group dropdown in a new `AppShell` header, `sonner`'s `Toaster`, and `@pierre/diffs`' `PatchDiff`.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind v4 (`@custom-variant dark (&:is(.dark *))`), `radix-ui` (re-exported `DropdownMenu`), `lucide-react@1.41.0`, `sonner@2.0.8`, `@pierre/diffs@1.4.1`, Vitest + jsdom + Testing Library.

**Spec:** `docs/tasks/task-002-theme-mode-selection/design.md` (PRD: `docs/tasks/task-002-theme-mode-selection/prd.md`)

## Global Constraints

- All work is in `apps/frontend`. **No Go file is edited.** Every command below runs with cwd = `apps/frontend` unless it says "repository root".
- Node is not always on `PATH`. If `npm` is missing: `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.
- Storage key is exactly `converge.theme`. Values are exactly `light`, `dark`, `system`. Default is `system`.
- `ThemePreference = "light" | "dark" | "system"`; `ResolvedTheme = "light" | "dark"`. The literal `"system"` must never be passed to `applyTheme`, to `PatchDiff`'s `themeType`, or to `Toaster`'s `theme`.
- `src/components/ui/**` is ignored by both `eslint.config.js` and `.prettierignore`. Everything else must satisfy `npm run lint` and `npm run format:check`. `index.html` is **not** ignored — run `npm run format` after editing it.
- `react-refresh/only-export-components` is enabled: a module must not export both a component and a non-component. This is why the context object, the hook, and the provider are three files.
- Work happens in the existing worktree `.worktrees/task-002-theme-mode-selection` on branch `task-002-theme-mode-selection`. Never create a new worktree.
- Every commit ends with the trailer `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- 97 tests pass before this plan starts. No task may leave the suite red.

---

## File Structure

| File | Kind | Responsibility |
| --- | --- | --- |
| `src/test/matchMedia.ts` | create | Controllable `window.matchMedia` stub; `setSystemDark` lever |
| `src/test/setup.ts` | modify | Install stubs per test; reset DOM/storage/stub after each |
| `src/test/__tests__/matchMedia.test.ts` | create | Proves the stub reports and notifies correctly |
| `src/lib/theme/types.ts` | create | `ThemePreference`, `ResolvedTheme`, `THEME_STORAGE_KEY`, `DEFAULT_PREFERENCE`, `isThemePreference` |
| `src/lib/theme/apply.ts` | create | `prefersDark`, `darkMediaQuery`, `resolveTheme`, `applyTheme` |
| `src/lib/theme/storage.ts` | create | `readStoredPreference`, `writeStoredPreference` — total, never throw |
| `src/lib/theme/context.ts` | create | `ThemeContext` + `ThemeContextValue` |
| `src/lib/theme/useTheme.ts` | create | Consumer hook; throws outside a provider |
| `src/lib/theme/__tests__/theme.test.ts` | create | Core unit tests |
| `src/components/theme/ThemeProvider.tsx` | create | State, OS subscription, DOM application |
| `src/components/theme/__tests__/ThemeProvider.test.tsx` | create | Provider behaviour |
| `src/components/theme/ThemeToggle.tsx` | create | The picker |
| `src/components/theme/__tests__/ThemeToggle.test.tsx` | create | Picker behaviour |
| `src/components/ui/dropdown-menu.tsx` | create | Vendored shadcn/Radix primitive |
| `src/components/layout/AppShell.tsx` | create | Sticky top bar + content slot |
| `src/components/layout/__tests__/AppShell.test.tsx` | create | Shell renders wordmark, control, children |
| `src/test/render.tsx` | modify | `ThemeProvider` inside the existing provider nesting |
| `src/__tests__/routes.test.tsx` | modify | Hand-built provider tree gains `ThemeProvider` |
| `src/components/features/review/__tests__/FileDiff.test.tsx` | modify | `renderWithProviders`; new theme-forwarding assertions |
| `index.html` | modify | Pre-paint boot script |
| `src/__tests__/bootThemeScript.test.ts` | create | Locks the boot script's invariants to `THEME_STORAGE_KEY` |
| `src/index.css` | modify | `color-scheme` split across `:root` / `.dark` |
| `src/App.tsx` | modify | Mount provider + shell; `ThemedToaster` |
| `src/components/features/review/FileDiff.tsx` | modify | Forward `themeType` + `theme` into `PatchDiff` |

---

## Task 1: Test harness — `matchMedia` and `ResizeObserver` stubs

jsdom implements neither. `matchMedia` is required by every theme test. `ResizeObserver` is required by Radix's dropdown: `@radix-ui/react-dropdown-menu` → `@radix-ui/react-menu` → `@radix-ui/react-popper` → `@radix-ui/react-use-size`, which calls `new ResizeObserver(...)` (verified in `node_modules/@radix-ui/react-use-size/dist/index.mjs:12`). Without it the picker test throws `ResizeObserver is not defined`.

**Files:**
- Create: `src/test/matchMedia.ts`
- Create: `src/test/__tests__/matchMedia.test.ts`
- Modify: `src/test/setup.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `installMatchMedia(): void`, `removeMatchMedia(): void`, `setSystemDark(dark: boolean): void`, `resetMatchMedia(): void`, `darkQueryListeners: { addEventListener: Mock; removeEventListener: Mock; count(): number }`. Later tasks use `setSystemDark` to drive FR-5.1/FR-5.2 and `darkQueryListeners.removeEventListener` to drive FR-5.3.

- [ ] **Step 1: Write the stub**

Create `src/test/matchMedia.ts`:

```ts
import { vi } from "vitest";

const DARK_QUERY = "(prefers-color-scheme: dark)";

type ChangeListener = (event: MediaQueryListEvent) => void;

let systemIsDark = false;
const listeners = new Set<ChangeListener>();

const addEventListener = vi.fn((type: string, listener: ChangeListener) => {
  if (type === "change") listeners.add(listener);
});

const removeEventListener = vi.fn((type: string, listener: ChangeListener) => {
  if (type === "change") listeners.delete(listener);
});

/**
 * darkQueryListeners exposes the stub's subscription calls so a test can assert
 * that a listener was torn down (FR-5.3) without depending on call ordering.
 */
export const darkQueryListeners = {
  addEventListener,
  removeEventListener,
  count: () => listeners.size,
};

/** installMatchMedia defines a controllable window.matchMedia. jsdom has none. */
export function installMatchMedia(): void {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: vi.fn((query: string) => ({
      media: query,
      get matches() {
        return query === DARK_QUERY ? systemIsDark : false;
      },
      onchange: null,
      addEventListener,
      removeEventListener,
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    })),
  });
}

/** removeMatchMedia deletes the property entirely, for the FR-1.4 fallback test. */
export function removeMatchMedia(): void {
  Reflect.deleteProperty(window as unknown as Record<string, unknown>, "matchMedia");
}

/** setSystemDark flips the OS preference and notifies every live listener. */
export function setSystemDark(dark: boolean): void {
  systemIsDark = dark;
  const event = { matches: dark, media: DARK_QUERY } as MediaQueryListEvent;
  for (const listener of [...listeners]) listener(event);
}

/** resetMatchMedia returns the stub to "OS is light, nobody subscribed". */
export function resetMatchMedia(): void {
  systemIsDark = false;
  listeners.clear();
  addEventListener.mockClear();
  removeEventListener.mockClear();
}
```

- [ ] **Step 2: Update the global setup**

Replace `src/test/setup.ts` entirely:

```ts
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";
import { installMatchMedia, resetMatchMedia } from "./matchMedia";

/**
 * jsdom implements neither matchMedia (theme resolution) nor ResizeObserver
 * (Radix's Popper, which backs dropdown-menu). Install both for every test.
 */
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeEach(() => {
  installMatchMedia();
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
});

afterEach(() => {
  cleanup();
  resetMatchMedia();
  localStorage.clear();
  // jsdom shares one document per file; a test that went dark must not leak.
  document.documentElement.className = "";
  document.documentElement.style.colorScheme = "";
});
```

- [ ] **Step 3: Write the failing test**

Create `src/test/__tests__/matchMedia.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { darkQueryListeners, removeMatchMedia, setSystemDark } from "@/test/matchMedia";

const DARK_QUERY = "(prefers-color-scheme: dark)";

describe("matchMedia stub", () => {
  it("reports light by default", () => {
    expect(window.matchMedia(DARK_QUERY).matches).toBe(false);
  });

  it("reports dark after setSystemDark(true)", () => {
    setSystemDark(true);
    expect(window.matchMedia(DARK_QUERY).matches).toBe(true);
  });

  it("reports false for an unrelated query", () => {
    setSystemDark(true);
    expect(window.matchMedia("(min-width: 600px)").matches).toBe(false);
  });

  it("notifies change listeners with the new value", () => {
    const listener = vi.fn();
    window.matchMedia(DARK_QUERY).addEventListener("change", listener);

    setSystemDark(true);

    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].matches).toBe(true);
    expect(darkQueryListeners.count()).toBe(1);
  });

  it("stops notifying a removed listener", () => {
    const listener = vi.fn();
    const query = window.matchMedia(DARK_QUERY);
    query.addEventListener("change", listener);
    query.removeEventListener("change", listener);

    setSystemDark(true);

    expect(listener).not.toHaveBeenCalled();
    expect(darkQueryListeners.count()).toBe(0);
  });

  it("can be removed entirely for the no-matchMedia fallback", () => {
    removeMatchMedia();
    expect(window.matchMedia).toBeUndefined();
  });
});
```

- [ ] **Step 4: Run the test**

Run: `npm test -- src/test/__tests__/matchMedia.test.ts`
Expected: 6 passing. If `reports light by default` fails with `window.matchMedia is not a function`, the `beforeEach` in `setup.ts` is not running — check `setupFiles` in `vitest.config.ts` points at `./src/test/setup.ts`.

- [ ] **Step 5: Confirm nothing regressed**

Run: `npm test`
Expected: the 97 pre-existing tests plus the 6 new ones, all passing. The new `afterEach` clears `localStorage`; no existing test depends on storage surviving a test boundary, so this must be clean. If any existing test fails, stop and report — do not weaken the reset.

- [ ] **Step 6: Lint and format**

Run: `npm run lint && npm run format:check`
Expected: clean. If `format:check` complains, run `npm run format` and re-check.

- [ ] **Step 7: Commit**

```bash
git add src/test/matchMedia.ts src/test/setup.ts src/test/__tests__/matchMedia.test.ts
git commit -m "test(frontend): add matchMedia and ResizeObserver stubs for theming" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Theme core — types, resolution, DOM application, storage

Pure functions, no React, no renderer needed. This is the module the boot script mirrors.

**Files:**
- Create: `src/lib/theme/types.ts`
- Create: `src/lib/theme/apply.ts`
- Create: `src/lib/theme/storage.ts`
- Test: `src/lib/theme/__tests__/theme.test.ts`

**Interfaces:**
- Consumes: Task 1's stubs (`setSystemDark`, `removeMatchMedia`).
- Produces:
  - `type ThemePreference = "light" | "dark" | "system"`
  - `type ResolvedTheme = "light" | "dark"`
  - `const THEME_STORAGE_KEY = "converge.theme"`
  - `const DEFAULT_PREFERENCE: ThemePreference = "system"`
  - `isThemePreference(value: unknown): value is ThemePreference`
  - `prefersDark(): boolean`
  - `darkMediaQuery(): MediaQueryList | null`
  - `resolveTheme(preference: ThemePreference, systemIsDark: boolean): ResolvedTheme`
  - `applyTheme(resolved: ResolvedTheme): void`
  - `readStoredPreference(): ThemePreference`
  - `writeStoredPreference(preference: ThemePreference): void`

- [ ] **Step 1: Write the failing test**

Create `src/lib/theme/__tests__/theme.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_PREFERENCE,
  THEME_STORAGE_KEY,
  isThemePreference,
} from "@/lib/theme/types";
import { applyTheme, prefersDark, resolveTheme } from "@/lib/theme/apply";
import { readStoredPreference, writeStoredPreference } from "@/lib/theme/storage";
import { removeMatchMedia, setSystemDark } from "@/test/matchMedia";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("isThemePreference", () => {
  it("accepts exactly the three modes", () => {
    expect(isThemePreference("light")).toBe(true);
    expect(isThemePreference("dark")).toBe(true);
    expect(isThemePreference("system")).toBe(true);
  });

  it("rejects anything else", () => {
    for (const value of ["DARK", "twilight", "", null, undefined, 0, {}]) {
      expect(isThemePreference(value)).toBe(false);
    }
  });
});

describe("resolveTheme", () => {
  it("returns the preference verbatim for light and dark, ignoring the OS", () => {
    expect(resolveTheme("light", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
    expect(resolveTheme("dark", true)).toBe("dark");
  });

  it("follows the OS under system", () => {
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("system", true)).toBe("dark");
  });
});

describe("prefersDark", () => {
  it("is false when the OS is light", () => {
    expect(prefersDark()).toBe(false);
  });

  it("is true when the OS is dark", () => {
    setSystemDark(true);
    expect(prefersDark()).toBe(true);
  });

  it("is false when matchMedia does not exist", () => {
    removeMatchMedia();
    expect(prefersDark()).toBe(false);
  });

  it("is false when matchMedia throws", () => {
    vi.spyOn(window, "matchMedia").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(prefersDark()).toBe(false);
  });
});

describe("applyTheme", () => {
  it("adds the dark class and sets color-scheme for dark", () => {
    applyTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("removes the dark class and sets color-scheme for light", () => {
    applyTheme("dark");
    applyTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe("light");
  });

  it("is idempotent", () => {
    applyTheme("dark");
    applyTheme("dark");
    expect(document.documentElement.className).toBe("dark");
  });

  it("preserves unrelated classes already on the root", () => {
    document.documentElement.classList.add("js-enabled");
    applyTheme("dark");
    expect(document.documentElement.classList.contains("js-enabled")).toBe(true);
    applyTheme("light");
    expect(document.documentElement.classList.contains("js-enabled")).toBe(true);
  });
});

describe("storage", () => {
  it("round-trips each preference", () => {
    for (const preference of ["light", "dark", "system"] as const) {
      writeStoredPreference(preference);
      expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe(preference);
      expect(readStoredPreference()).toBe(preference);
    }
  });

  it("falls back to the default when the key is absent", () => {
    expect(readStoredPreference()).toBe(DEFAULT_PREFERENCE);
    expect(DEFAULT_PREFERENCE).toBe("system");
  });

  it("falls back to the default for an unrecognized value", () => {
    for (const corrupt of ["DARK", "twilight", "", "{}"]) {
      localStorage.setItem(THEME_STORAGE_KEY, corrupt);
      expect(readStoredPreference()).toBe("system");
    }
  });

  it("falls back to the default when getItem throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("storage disabled");
    });
    expect(readStoredPreference()).toBe("system");
  });

  it("swallows a throwing setItem", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });
    expect(() => writeStoredPreference("dark")).not.toThrow();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- src/lib/theme/__tests__/theme.test.ts`
Expected: FAIL — `Failed to resolve import "@/lib/theme/types"`.

- [ ] **Step 3: Write `types.ts`**

Create `src/lib/theme/types.ts`:

```ts
/** ThemePreference is what the user chose. */
export type ThemePreference = "light" | "dark" | "system";

/** ResolvedTheme is what is actually applied to the DOM. */
export type ResolvedTheme = "light" | "dark";

/**
 * THEME_STORAGE_KEY is the localStorage key holding the preference.
 *
 * DUPLICATED in the pre-paint boot script in `apps/frontend/index.html`, which
 * runs before any module can load and therefore cannot import this. Change one,
 * change the other (FR-4.4). `src/__tests__/bootThemeScript.test.ts` fails if
 * they drift.
 */
export const THEME_STORAGE_KEY = "converge.theme";

/** DEFAULT_PREFERENCE applies when nothing valid is stored. */
export const DEFAULT_PREFERENCE: ThemePreference = "system";

/**
 * isThemePreference is the allowlist guard. Every value read from storage passes
 * through here before it can reach the DOM, so a tampered storage value can
 * never become an arbitrary class name or color-scheme value.
 */
export function isThemePreference(value: unknown): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}
```

- [ ] **Step 4: Write `apply.ts`**

Create `src/lib/theme/apply.ts`:

```ts
import type { ResolvedTheme, ThemePreference } from "@/lib/theme/types";

const DARK_QUERY = "(prefers-color-scheme: dark)";

/**
 * darkMediaQuery returns the OS dark-mode query, or null when matchMedia is
 * unavailable or throws (FR-1.4).
 */
export function darkMediaQuery(): MediaQueryList | null {
  try {
    return typeof window.matchMedia === "function" ? window.matchMedia(DARK_QUERY) : null;
  } catch {
    return null;
  }
}

/** prefersDark reports whether the OS currently prefers dark. Defaults to false. */
export function prefersDark(): boolean {
  return darkMediaQuery()?.matches ?? false;
}

/** resolveTheme maps a preference plus the OS bit onto the theme to apply. */
export function resolveTheme(preference: ThemePreference, systemIsDark: boolean): ResolvedTheme {
  if (preference === "system") return systemIsDark ? "dark" : "light";
  return preference;
}

/**
 * applyTheme is the only runtime writer of the theme to the DOM.
 *
 * DUPLICATED, by hand, in the pre-paint boot script in `apps/frontend/index.html`
 * (FR-4.4). Change one, change the other.
 *
 * `classList.toggle` touches exactly one token, so unrelated root classes
 * survive. The function is idempotent, so the provider re-applying what the boot
 * script already wrote is a no-op rather than a clear-then-restore flicker.
 */
export function applyTheme(resolved: ResolvedTheme): void {
  const root = document.documentElement;
  root.classList.toggle("dark", resolved === "dark");
  root.style.colorScheme = resolved;
}
```

- [ ] **Step 5: Write `storage.ts`**

Create `src/lib/theme/storage.ts`:

```ts
import {
  DEFAULT_PREFERENCE,
  THEME_STORAGE_KEY,
  isThemePreference,
  type ThemePreference,
} from "@/lib/theme/types";

/**
 * readStoredPreference is total: an absent key, a corrupt value, and a throwing
 * store all collapse to the default (FR-3.2, FR-3.3, FR-3.4).
 */
export function readStoredPreference(): ThemePreference {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    return isThemePreference(raw) ? raw : DEFAULT_PREFERENCE;
  } catch {
    return DEFAULT_PREFERENCE;
  }
}

/**
 * writeStoredPreference persists the literal preference — selecting System
 * stores "system", not the theme it happened to resolve to (FR-3.5).
 */
export function writeStoredPreference(preference: ThemePreference): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, preference);
  } catch {
    // Private browsing, disabled storage, or quota exhaustion. The session stays
    // correct in memory; a failed write must never block applying the theme.
  }
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `npm test -- src/lib/theme/__tests__/theme.test.ts`
Expected: all passing.

- [ ] **Step 7: Lint, format, typecheck**

Run: `npm run lint && npm run format:check && npm run build`
Expected: clean. `npm run build` runs `tsc` — it catches a type error the tests would not.

- [ ] **Step 8: Commit**

```bash
git add src/lib/theme
git commit -m "feat(frontend): add theme core types, resolution, and storage" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `ThemeProvider`, context, and hook

Three files because `react-refresh/only-export-components` forbids a module that exports both a component and a non-component.

The resolved theme is **derived during render**, never stored. That is what makes FR-5.2 free (`resolveTheme` ignores `systemIsDark` unless the preference is `system`) and makes it impossible for `preference` and `resolved` to disagree. The media-query listener is therefore always on, with an empty dependency list, rather than being re-registered on every preference change.

**Files:**
- Create: `src/lib/theme/context.ts`
- Create: `src/lib/theme/useTheme.ts`
- Create: `src/components/theme/ThemeProvider.tsx`
- Test: `src/components/theme/__tests__/ThemeProvider.test.tsx`

**Interfaces:**
- Consumes: `applyTheme`, `darkMediaQuery`, `prefersDark`, `resolveTheme` from `@/lib/theme/apply`; `readStoredPreference`, `writeStoredPreference` from `@/lib/theme/storage`; `ThemePreference`, `ResolvedTheme` from `@/lib/theme/types`.
- Produces:
  - `interface ThemeContextValue { preference: ThemePreference; resolved: ResolvedTheme; setPreference: (preference: ThemePreference) => void }`
  - `const ThemeContext: React.Context<ThemeContextValue | null>`
  - `useTheme(): ThemeContextValue` — throws `"useTheme must be used within a ThemeProvider"` outside one
  - `<ThemeProvider>{children}</ThemeProvider>`

- [ ] **Step 1: Write the failing test**

Create `src/components/theme/__tests__/ThemeProvider.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { describe, expect, it } from "vitest";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { useTheme } from "@/lib/theme/useTheme";
import { THEME_STORAGE_KEY, type ThemePreference } from "@/lib/theme/types";
import { darkQueryListeners, setSystemDark } from "@/test/matchMedia";

function Probe() {
  const { preference, resolved, setPreference } = useTheme();
  return (
    <div>
      <span data-testid="preference">{preference}</span>
      <span data-testid="resolved">{resolved}</span>
      {(["light", "dark", "system"] as ThemePreference[]).map((mode) => (
        <button key={mode} onClick={() => setPreference(mode)}>
          set {mode}
        </button>
      ))}
    </div>
  );
}

function renderProvider() {
  return render(
    <ThemeProvider>
      <Probe />
    </ThemeProvider>,
  );
}

function isDark() {
  return document.documentElement.classList.contains("dark");
}

describe("useTheme", () => {
  it("throws outside a provider", () => {
    expect(() => render(<Probe />)).toThrow(/must be used within a ThemeProvider/);
  });
});

describe("ThemeProvider", () => {
  it("defaults to system and resolves light when the OS is light", () => {
    renderProvider();
    expect(screen.getByTestId("preference")).toHaveTextContent("system");
    expect(screen.getByTestId("resolved")).toHaveTextContent("light");
    expect(isDark()).toBe(false);
  });

  it("boots from a stored dark preference and applies it", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    renderProvider();
    expect(screen.getByTestId("preference")).toHaveTextContent("dark");
    expect(isDark()).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("boots from a stored system preference against a dark OS", () => {
    setSystemDark(true);
    localStorage.setItem(THEME_STORAGE_KEY, "system");
    renderProvider();
    expect(screen.getByTestId("preference")).toHaveTextContent("system");
    expect(screen.getByTestId("resolved")).toHaveTextContent("dark");
    expect(isDark()).toBe(true);
  });

  it("treats a corrupt stored value as system", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "twilight");
    renderProvider();
    expect(screen.getByTestId("preference")).toHaveTextContent("system");
  });

  it("applies and persists a new preference on selection", async () => {
    const user = userEvent.setup();
    renderProvider();

    await user.click(screen.getByRole("button", { name: "set dark" }));

    expect(isDark()).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");

    await user.click(screen.getByRole("button", { name: "set light" }));

    expect(isDark()).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe("light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
  });

  it("persists the literal 'system', not the theme it resolved to", async () => {
    const user = userEvent.setup();
    setSystemDark(true);
    renderProvider();

    await user.click(screen.getByRole("button", { name: "set system" }));

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
    expect(screen.getByTestId("resolved")).toHaveTextContent("dark");
  });

  it("still applies the theme when persistence is unavailable", async () => {
    const user = userEvent.setup();
    const setItem = Storage.prototype.setItem;
    Storage.prototype.setItem = () => {
      throw new Error("storage disabled");
    };
    try {
      renderProvider();
      await user.click(screen.getByRole("button", { name: "set dark" }));
      expect(isDark()).toBe(true);
    } finally {
      Storage.prototype.setItem = setItem;
    }
  });

  it("follows a live OS change while on system", () => {
    renderProvider();
    expect(isDark()).toBe(false);

    act(() => setSystemDark(true));

    expect(screen.getByTestId("resolved")).toHaveTextContent("dark");
    expect(isDark()).toBe(true);

    act(() => setSystemDark(false));

    expect(isDark()).toBe(false);
  });

  it("ignores a live OS change while pinned to light", async () => {
    const user = userEvent.setup();
    renderProvider();
    await user.click(screen.getByRole("button", { name: "set light" }));

    act(() => setSystemDark(true));

    expect(screen.getByTestId("resolved")).toHaveTextContent("light");
    expect(isDark()).toBe(false);
  });

  it("ignores a live OS change while pinned to dark", async () => {
    const user = userEvent.setup();
    renderProvider();
    await user.click(screen.getByRole("button", { name: "set dark" }));

    act(() => setSystemDark(false));

    expect(screen.getByTestId("resolved")).toHaveTextContent("dark");
    expect(isDark()).toBe(true);
  });

  it("picks the OS value back up when returning to system", async () => {
    const user = userEvent.setup();
    renderProvider();
    await user.click(screen.getByRole("button", { name: "set light" }));
    act(() => setSystemDark(true));

    await user.click(screen.getByRole("button", { name: "set system" }));

    expect(screen.getByTestId("resolved")).toHaveTextContent("dark");
    expect(isDark()).toBe(true);
  });

  it("tears the media-query listener down on unmount", () => {
    const { unmount } = renderProvider();
    expect(darkQueryListeners.count()).toBe(1);

    unmount();

    expect(darkQueryListeners.removeEventListener).toHaveBeenCalled();
    expect(darkQueryListeners.count()).toBe(0);
  });

  it("survives a missing matchMedia", () => {
    const matchMedia = window.matchMedia;
    Reflect.deleteProperty(window as unknown as Record<string, unknown>, "matchMedia");
    try {
      renderProvider();
      expect(screen.getByTestId("resolved")).toHaveTextContent("light");
      expect(isDark()).toBe(false);
    } finally {
      window.matchMedia = matchMedia;
    }
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- src/components/theme/__tests__/ThemeProvider.test.tsx`
Expected: FAIL — `Failed to resolve import "@/components/theme/ThemeProvider"`.

- [ ] **Step 3: Write the context**

Create `src/lib/theme/context.ts`:

```ts
import { createContext } from "react";
import type { ResolvedTheme, ThemePreference } from "@/lib/theme/types";

export interface ThemeContextValue {
  /** What the user chose. Render the picker from this (FR-1.5, FR-6.2). */
  preference: ThemePreference;
  /** What is currently applied. Render theme-dependent UI from this (FR-6.5). */
  resolved: ResolvedTheme;
  /** The only supported way to change the theme at runtime. */
  setPreference: (preference: ThemePreference) => void;
}

export const ThemeContext = createContext<ThemeContextValue | null>(null);
```

- [ ] **Step 4: Write the hook**

Create `src/lib/theme/useTheme.ts`:

```ts
import { useContext } from "react";
import { ThemeContext, type ThemeContextValue } from "@/lib/theme/context";

/** useTheme reads the theme context. Throws outside a ThemeProvider, by design. */
export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext);
  if (!value) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return value;
}
```

- [ ] **Step 5: Write the provider**

Create `src/components/theme/ThemeProvider.tsx`:

```tsx
import { useCallback, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { ThemeContext, type ThemeContextValue } from "@/lib/theme/context";
import { applyTheme, darkMediaQuery, prefersDark, resolveTheme } from "@/lib/theme/apply";
import { readStoredPreference, writeStoredPreference } from "@/lib/theme/storage";
import type { ThemePreference } from "@/lib/theme/types";

/**
 * ThemeProvider owns the preference, tracks the OS preference, and applies the
 * resolved theme to the document.
 *
 * The resolved theme is derived during render rather than stored, so the
 * preference and what is applied can never disagree, and an OS change is inert
 * under an explicit Light/Dark choice without any code of its own (FR-5.2).
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
  // Lazy initializers: storage and matchMedia are read once at mount.
  const [preference, setPreferenceState] = useState<ThemePreference>(readStoredPreference);
  const [systemIsDark, setSystemIsDark] = useState<boolean>(prefersDark);

  const resolved = resolveTheme(preference, systemIsDark);

  // A layout effect, not an effect: it runs before the browser paints, so a
  // preference change commits in the same frame as the click (FR-2.3) and
  // mounting cannot produce a painted frame in the wrong theme (FR-4.3).
  useLayoutEffect(() => {
    applyTheme(resolved);
  }, [resolved]);

  useEffect(() => {
    const query = darkMediaQuery();
    if (!query) return;
    const onChange = (event: MediaQueryListEvent) => setSystemIsDark(event.matches);
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  const setPreference = useCallback((next: ThemePreference) => {
    setPreferenceState(next);
    writeStoredPreference(next);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ preference, resolved, setPreference }),
    [preference, resolved, setPreference],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `npm test -- src/components/theme/__tests__/ThemeProvider.test.tsx`
Expected: all passing.

Note on the `throws outside a provider` case: React logs the error to the console during the failed render. That is expected noise, not a failure. Do **not** silence it by loosening the assertion.

- [ ] **Step 7: Full suite, lint, format, build**

Run: `npm test && npm run lint && npm run format:check && npm run build`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add src/lib/theme/context.ts src/lib/theme/useTheme.ts src/components/theme
git commit -m "feat(frontend): add ThemeProvider, theme context, and useTheme hook" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Put `ThemeProvider` in the test render helpers

`useTheme` throws outside a provider, so every test that renders a theme consumer needs one. Doing this now — before any consumer exists — keeps later tasks from mixing a behaviour change with a harness change.

Two existing test files build their own trees and must be updated. No assertion in either changes (FR-7.3).

**Files:**
- Modify: `src/test/render.tsx`
- Modify: `src/__tests__/routes.test.tsx`
- Modify: `src/components/features/review/__tests__/FileDiff.test.tsx`

**Interfaces:**
- Consumes: `ThemeProvider` from `@/components/theme/ThemeProvider`.
- Produces: `renderWithProviders` and `queryWrapper` both wrap children in `ThemeProvider`. Later tasks rely on this.

- [ ] **Step 1: Add the provider to `renderWithProviders`**

In `src/test/render.tsx`, add the import:

```tsx
import { ThemeProvider } from "@/components/theme/ThemeProvider";
```

and replace the body of `renderWithProviders` with:

```tsx
export function renderWithProviders(
  ui: ReactElement,
  options: { route?: string } = {},
): RenderResult {
  const client = testClient();
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[options.route ?? "/"]}>{ui}</MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}
```

Leave `queryWrapper` alone: it wraps *hooks* under test, none of which consume theme context.

- [ ] **Step 2: Add the provider to the routes test's hand-built tree**

In `src/__tests__/routes.test.tsx`, add:

```tsx
import { ThemeProvider } from "@/components/theme/ThemeProvider";
```

and change `renderAt` to:

```tsx
function renderAt(route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[route]}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}
```

Both `it` blocks stay exactly as they are.

- [ ] **Step 3: Move `FileDiff.test.tsx` onto `renderWithProviders`**

In `src/components/features/review/__tests__/FileDiff.test.tsx`:

- change the Testing Library import to `import { screen } from "@testing-library/react";`
- add `import { renderWithProviders } from "@/test/render";`
- replace both `render(<FileDiff ... />)` calls with `renderWithProviders(<FileDiff ... />)`

Keep the existing explanatory `NOTE:` comment block and both assertions verbatim.

- [ ] **Step 4: Run the affected tests**

Run: `npm test -- src/__tests__/routes.test.tsx src/components/features/review/__tests__/FileDiff.test.tsx`
Expected: 4 passing, unchanged assertions.

- [ ] **Step 5: Run the whole suite**

Run: `npm test`
Expected: every pre-existing test still passes. If a page test now fails, the cause is the provider's `useLayoutEffect` touching `document.documentElement` — which `setup.ts` resets — so investigate rather than reverting the helper.

- [ ] **Step 6: Lint, format, build**

Run: `npm run lint && npm run format:check && npm run build`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add src/test/render.tsx src/__tests__/routes.test.tsx \
  src/components/features/review/__tests__/FileDiff.test.tsx
git commit -m "test(frontend): render tests inside ThemeProvider" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Pre-paint boot script and the `color-scheme` split

`:root { color-scheme: light dark; }` currently tells the browser the page handles both, which is true of the tokens and false of the app. Split it so the stylesheet alone is a correct no-JS floor; `applyTheme` also writes the inline style, which wins on specificity and is in effect before the stylesheet is parsed.

The boot script must be a **classic** inline script — `type="module"` is deferred and always runs after first paint, which defeats the entire purpose.

**Files:**
- Modify: `src/index.css` (the `:root` block's first declaration, and the `.dark` block)
- Modify: `index.html`
- Test: `src/__tests__/bootThemeScript.test.ts`

**Interfaces:**
- Consumes: `THEME_STORAGE_KEY` from `@/lib/theme/types` (the test imports it; the script hard-codes it).
- Produces: nothing importable. The guarantee is that `document.documentElement` already carries the right `dark` class and `color-scheme` before the React bundle runs.

- [ ] **Step 1: Write the failing test**

Create `src/__tests__/bootThemeScript.test.ts`. This is a source-level regression guard: it is the mechanism that makes FR-4.4's "these two must not drift" enforceable rather than aspirational.

```ts
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";

const html = readFileSync(path.resolve(import.meta.dirname, "../../index.html"), "utf8");

describe("pre-paint theme boot script in index.html", () => {
  it("reads the same storage key the TypeScript core uses", () => {
    expect(html).toContain(`localStorage.getItem("${THEME_STORAGE_KEY}")`);
  });

  it("validates the stored value against the three-mode allowlist", () => {
    expect(html).toContain('pref !== "light"');
    expect(html).toContain('pref !== "dark"');
    expect(html).toContain('pref !== "system"');
  });

  it("writes the dark class and color-scheme onto the document root", () => {
    expect(html).toContain('classList.toggle("dark"');
    expect(html).toContain("style.colorScheme");
  });

  it("runs before the application module script", () => {
    const bootIndex = html.indexOf("localStorage.getItem");
    const moduleIndex = html.indexOf('<script type="module"');
    expect(bootIndex).toBeGreaterThan(-1);
    expect(moduleIndex).toBeGreaterThan(-1);
    expect(bootIndex).toBeLessThan(moduleIndex);
  });

  it("is a classic script, not a deferred module", () => {
    const boot = html.slice(0, html.indexOf("localStorage.getItem"));
    const openingTag = boot.slice(boot.lastIndexOf("<script"));
    expect(openingTag).not.toContain("type=");
    expect(openingTag).not.toContain("defer");
    expect(openingTag).not.toContain("async");
  });

  it("guards against a throwing storage or matchMedia", () => {
    expect(html).toContain("try {");
    expect(html).toContain("} catch {");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- src/__tests__/bootThemeScript.test.ts`
Expected: FAIL on the first assertion — `index.html` has no boot script yet.

- [ ] **Step 3: Add the boot script to `index.html`**

Replace `index.html` entirely. The script is the first child of `<head>`, before the `<title>` and before anything that could paint:

```html
<!doctype html>
<html lang="en">
  <head>
    <script>
      // Pre-paint theme application. This is a hand-inlined copy of resolveTheme
      // and applyTheme from src/lib/theme/apply.ts, plus the storage key from
      // src/lib/theme/types.ts. A module script is deferred and cannot run before
      // first paint, so this cannot import them. Change one, change the other.
      // src/__tests__/bootThemeScript.test.ts fails if they drift.
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
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Converge</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 4: Split `color-scheme` in the stylesheet**

In `src/index.css`, change the first declaration inside `:root` from

```css
  color-scheme: light dark;
```

to

```css
  color-scheme: light;
```

and add `color-scheme: dark;` as the first declaration inside the existing `.dark` block, immediately before `--background: oklch(0.145 0 0);`:

```css
.dark {
  color-scheme: dark;
  --background: oklch(0.145 0 0);
```

Do not touch any token value.

- [ ] **Step 5: Format, then run the test**

`index.html` is not in `.prettierignore`, so an unformatted inline script fails `format:check`.

Run: `npm run format && npm test -- src/__tests__/bootThemeScript.test.ts && npm run format:check`
Expected: 6 passing, `format:check` clean. If Prettier reflowed the script such that an assertion no longer matches (for example splitting `localStorage.getItem("converge.theme")` across lines), adjust the **test** to match Prettier's output — never disable Prettier for this file.

- [ ] **Step 6: Verify the built output keeps the script inline**

Run: `npm run build && grep -c "converge.theme" ../backend/internal/ui/dist/index.html`
Expected: `1`. Vite leaves inline classic scripts alone; if the count is `0`, stop and report — the whole flash-prevention requirement depends on it.

- [ ] **Step 7: Full suite and lint**

Run: `npm test && npm run lint`
Expected: clean.

- [ ] **Step 8: Commit**

`npm run build` writes into `apps/backend/internal/ui/dist`, which is gitignored (`.gitignore:45`, only `.gitkeep` is tracked) — do not try to stage it.

```bash
git add index.html src/index.css src/__tests__/bootThemeScript.test.ts
git commit -m "feat(frontend): apply the stored theme before first paint" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: The `dropdown-menu` primitive and `ThemeToggle`

The picker uses `DropdownMenuRadioGroup` / `DropdownMenuRadioItem`, not plain items. Radix then emits `role="menuitemradio"` with `aria-checked`, so "the current preference is marked" is announced to screen readers rather than merely drawn, and is assertable as `getByRole("menuitemradio", { name: /system/i, checked: true })`.

The trigger icon reflects **`resolved`** (what the app looks like now, FR-6.5); the checked item reflects **`preference`** (what the user chose, FR-6.2). These differ when System is selected — that is the point of FR-1.5.

No new dependency: `radix-ui@1.6.7` re-exports `DropdownMenu` from `@radix-ui/react-dropdown-menu` (`node_modules/radix-ui/dist/index.d.ts:21-22`), the same package `select.tsx` imports from, and `@radix-ui/react-dropdown-menu` is already installed.

**Files:**
- Create: `src/components/ui/dropdown-menu.tsx`
- Create: `src/components/theme/ThemeToggle.tsx`
- Test: `src/components/theme/__tests__/ThemeToggle.test.tsx`

**Interfaces:**
- Consumes: `useTheme` from `@/lib/theme/useTheme`; `isThemePreference` from `@/lib/theme/types`; `Button` from `@/components/ui/button` (`variant="ghost"` and `size="icon"` both exist, `size-8`).
- Produces: `<ThemeToggle />`, no props. Exports from `dropdown-menu.tsx`: `DropdownMenu`, `DropdownMenuTrigger`, `DropdownMenuPortal`, `DropdownMenuContent`, `DropdownMenuRadioGroup`, `DropdownMenuRadioItem`.

- [ ] **Step 1: Vendor the primitive**

First try the registry:

```bash
npx shadcn@latest add dropdown-menu
```

It writes `src/components/ui/dropdown-menu.tsx` in the `radix-nova` style from `components.json`. Take the generated file unmodified — `src/components/ui/**` is lint- and format-ignored. If it also rewrote `src/index.css` or `components.json`, revert those two files (`git checkout -- src/index.css components.json`) and keep only the new component.

If the registry is unreachable, hand-write exactly this file instead. It mirrors `select.tsx`'s shape: same `import { X as XPrimitive } from "radix-ui"` form, same `data-slot` attributes, same semantic token classes. Only the parts `ThemeToggle` uses are included.

```tsx
"use client"

import * as React from "react"
import { cn } from "@/lib/utils"
import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui"
import { CheckIcon } from "lucide-react"

function DropdownMenu({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Root>) {
  return <DropdownMenuPrimitive.Root data-slot="dropdown-menu" {...props} />
}

function DropdownMenuTrigger({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Trigger>) {
  return (
    <DropdownMenuPrimitive.Trigger data-slot="dropdown-menu-trigger" {...props} />
  )
}

function DropdownMenuPortal({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Portal>) {
  return <DropdownMenuPrimitive.Portal data-slot="dropdown-menu-portal" {...props} />
}

function DropdownMenuContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        data-slot="dropdown-menu-content"
        sideOffset={sideOffset}
        className={cn(
          "z-50 max-h-(--radix-dropdown-menu-content-available-height) min-w-32 origin-(--radix-dropdown-menu-content-transform-origin) overflow-x-hidden overflow-y-auto rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-md data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95",
          className
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

function DropdownMenuRadioGroup({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioGroup>) {
  return (
    <DropdownMenuPrimitive.RadioGroup
      data-slot="dropdown-menu-radio-group"
      {...props}
    />
  )
}

function DropdownMenuRadioItem({
  className,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioItem>) {
  return (
    <DropdownMenuPrimitive.RadioItem
      data-slot="dropdown-menu-radio-item"
      className={cn(
        "relative flex cursor-pointer items-center gap-2 rounded-md py-1.5 pr-8 pl-2 text-sm outline-none select-none focus:bg-accent focus:text-accent-foreground data-disabled:pointer-events-none data-disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    >
      {children}
      <span className="absolute right-2 flex size-3.5 items-center justify-center">
        <DropdownMenuPrimitive.ItemIndicator>
          <CheckIcon className="size-4" />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
    </DropdownMenuPrimitive.RadioItem>
  )
}

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuPortal,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
}
```

- [ ] **Step 2: Confirm the primitive typechecks**

Run: `npm run build`
Expected: clean. A failure here means the `radix-ui` re-export shape differs from what the file assumes — check `node_modules/radix-ui/dist/index.d.ts` before changing anything else.

- [ ] **Step 3: Write the failing test**

Create `src/components/theme/__tests__/ThemeToggle.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ThemeToggle } from "@/components/theme/ThemeToggle";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";
import { renderWithProviders } from "@/test/render";
import { setSystemDark } from "@/test/matchMedia";

async function openMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /change theme/i }));
  await screen.findByRole("menu");
}

describe("ThemeToggle", () => {
  it("exposes an accessible trigger", () => {
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
  });

  it("offers exactly three modes", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    const items = screen.getAllByRole("menuitemradio");
    expect(items).toHaveLength(3);
    expect(items.map((item) => item.textContent)).toEqual(["Light", "Dark", "System"]);
  });

  it("marks the stored preference as checked", async () => {
    const user = userEvent.setup();
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    expect(screen.getByRole("menuitemradio", { name: "Dark", checked: true })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: "Light", checked: false })).toBeInTheDocument();
    expect(
      screen.getByRole("menuitemradio", { name: "System", checked: false }),
    ).toBeInTheDocument();
  });

  it("keeps System checked while the trigger shows the resolved theme", async () => {
    const user = userEvent.setup();
    setSystemDark(true);
    renderWithProviders(<ThemeToggle />);

    // FR-6.5: the trigger reflects `resolved` (dark), so the moon is shown...
    expect(screen.getByRole("button", { name: /change theme/i })).toContainHTML("lucide-moon");

    await openMenu(user);

    // ...while FR-6.2 keeps the checked item on `preference` (system), not dark.
    expect(
      screen.getByRole("menuitemradio", { name: "System", checked: true }),
    ).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: "Dark", checked: false })).toBeInTheDocument();
  });

  it("shows the sun on the trigger when the resolved theme is light", () => {
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole("button", { name: /change theme/i })).toContainHTML("lucide-sun");
  });

  it("applies, persists, and closes the menu on selection", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    await user.click(screen.getByRole("menuitemradio", { name: "Dark" }));

    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());
  });

  it("persists the literal 'system' when System is selected", async () => {
    const user = userEvent.setup();
    setSystemDark(true);
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    await user.click(screen.getByRole("menuitemradio", { name: "System" }));

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("opens and selects by keyboard alone", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);

    await user.tab();
    expect(screen.getByRole("button", { name: /change theme/i })).toHaveFocus();

    await user.keyboard("{Enter}");
    await screen.findByRole("menu");
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");

    await waitFor(() => expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark"));
  });

  it("dismisses with Escape without changing the theme", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });
});
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `npm test -- src/components/theme/__tests__/ThemeToggle.test.tsx`
Expected: FAIL — `Failed to resolve import "@/components/theme/ThemeToggle"`.

- [ ] **Step 5: Write `ThemeToggle`**

Create `src/components/theme/ThemeToggle.tsx`:

```tsx
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "@/lib/theme/useTheme";
import { isThemePreference } from "@/lib/theme/types";

/**
 * ThemeToggle is the Light / Dark / System picker.
 *
 * The trigger icon shows the *resolved* theme — what the app looks like right
 * now — while the checked item shows the *preference*. Those deliberately
 * differ under System.
 */
export function ThemeToggle() {
  const { preference, resolved, setPreference } = useTheme();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Change theme">
          {resolved === "dark" ? <MoonIcon /> : <SunIcon />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuRadioGroup
          value={preference}
          onValueChange={(value) => {
            if (isThemePreference(value)) setPreference(value);
          }}
        >
          <DropdownMenuRadioItem value="light">
            <SunIcon />
            Light
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <MoonIcon />
            Dark
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">
            <MonitorIcon />
            System
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `npm test -- src/components/theme/__tests__/ThemeToggle.test.tsx`
Expected: all passing.

Troubleshooting, in order of likelihood:

- `ResizeObserver is not defined` → Task 1's `vi.stubGlobal` is not in effect; check `src/test/setup.ts`.
- `toContainHTML("lucide-moon")` fails → check the rendered markup for the actual class `lucide-react` emits for these icons (`lucide-react@1.41.0`) and assert on that string instead. The intent is "the trigger shows the moon when dark"; keep the intent, fix the selector.
- The three item `textContent` values include stray whitespace → trim in the assertion rather than removing the icons.

- [ ] **Step 7: Full suite, lint, format, build**

Run: `npm test && npm run lint && npm run format:check && npm run build`
Expected: clean. `dropdown-menu.tsx` is exempt from lint and format but not from `tsc`.

- [ ] **Step 8: Commit**

```bash
git add src/components/ui/dropdown-menu.tsx src/components/theme/ThemeToggle.tsx \
  src/components/theme/__tests__/ThemeToggle.test.tsx
git commit -m "feat(frontend): add the theme picker and dropdown-menu primitive" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: `AppShell`

A plain wrapper component, not a React Router layout route. The shell applies to every path including the `*` not-found branch and needs no route data, so wrapping in `App.tsx` keeps `routes.tsx` a pure route table and leaves `routes.test.tsx` structurally untouched.

The header is `sticky`, not `fixed`, so it occupies layout space instead of overlaying the diff (FR-7.5), and declares no `max-w-*` of its own — every page keeps its existing `mx-auto max-w-*` container, including `ReviewPage`'s `max-w-7xl`. The shell renders no `PageHeader` and owns no title (FR-7.4). `html, body, #root { height: 100% }` already exists in `index.css`, so `min-h-full` resolves.

**Files:**
- Create: `src/components/layout/AppShell.tsx`
- Test: `src/components/layout/__tests__/AppShell.test.tsx`

**Interfaces:**
- Consumes: `ThemeToggle` from `@/components/theme/ThemeToggle`; `Link` from `react-router`.
- Produces: `<AppShell>{children}</AppShell>` — `{ children: ReactNode }`.

- [ ] **Step 1: Write the failing test**

Create `src/components/layout/__tests__/AppShell.test.tsx`:

```tsx
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { renderWithProviders } from "@/test/render";

describe("AppShell", () => {
  it("renders its children", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByText("page content")).toBeInTheDocument();
  });

  it("renders the wordmark as a link home", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("link", { name: "Converge" })).toHaveAttribute("href", "/");
  });

  it("renders the theme control", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
  });

  it("puts the children in a main landmark below the banner", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(screen.getByRole("main")).toContainElement(screen.getByText("page content"));
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- src/components/layout/__tests__/AppShell.test.tsx`
Expected: FAIL — `Failed to resolve import "@/components/layout/AppShell"`.

- [ ] **Step 3: Write `AppShell`**

Create `src/components/layout/AppShell.tsx`:

```tsx
import type { ReactNode } from "react";
import { Link } from "react-router";
import { ThemeToggle } from "@/components/theme/ThemeToggle";

/**
 * AppShell is the persistent application chrome: a wordmark and the theme
 * control, above the routed page.
 *
 * It owns no title or description — pages keep rendering their own PageHeader —
 * and it sets no max width, so each page's own container still governs layout.
 * The header is sticky rather than fixed so it never overlays page content.
 */
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-40 flex h-14 items-center justify-between border-b border-border bg-background px-4">
        <Link to="/" className="text-sm font-semibold text-foreground">
          Converge
        </Link>
        <ThemeToggle />
      </header>
      <main className="flex-1">{children}</main>
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npm test -- src/components/layout/__tests__/AppShell.test.tsx`
Expected: 4 passing.

- [ ] **Step 5: Full suite, lint, format, build**

Run: `npm test && npm run lint && npm run format:check && npm run build`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add src/components/layout
git commit -m "feat(frontend): add the application shell with theme control" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Wire the provider, shell, and themed `Toaster` into `App.tsx`

`ThemeProvider` sits **inside** `QueryClientProvider`. The `queryClient` is a module-level singleton, so its cache is immune to re-renders either way, but keeping the provider below it matches the existing nesting and makes the dependency direction obvious. A theme change re-renders the subtree below `ThemeProvider` — which is the UI, and must re-render anyway to pick up the new value. No query is invalidated and no fetch is triggered.

`ThemedToaster` exists because `Toaster` must be inside the provider to read context, and isolating the read keeps a theme change from being the reason `App` itself re-renders.

**Files:**
- Modify: `src/App.tsx`
- Test: `src/__tests__/App.test.tsx` (create)

**Interfaces:**
- Consumes: `ThemeProvider`, `AppShell`, `useTheme`.
- Produces: nothing importable beyond the existing `App`.

- [ ] **Step 1: Write the failing test**

Create `src/__tests__/App.test.tsx`.

`App` mounts a real `BrowserRouter` and the real module-level `queryClient`, so every test here first points jsdom's URL at an unmatched path. That renders the not-found branch — which is still inside `AppShell` and still renders `ThemedToaster` — without mounting a page that fetches. MSW is started per-file in this repo, not globally, so a route that issues queries would hit the network.

```tsx
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { App } from "@/App";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";

beforeEach(() => {
  window.history.pushState({}, "", "/no-such-page");
});

describe("App", () => {
  it("renders the shell chrome around the routed page", () => {
    render(<App />);
    expect(screen.getByRole("link", { name: "Converge" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
    expect(screen.getByRole("main")).toContainElement(screen.getByText("Page not found"));
  });

  it("applies the stored theme on mount", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    render(<App />);
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("gives the toaster the resolved theme, never the literal system", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "system");
    render(<App />);
    const toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).not.toBeNull();
    expect(toaster?.getAttribute("data-theme")).toBe("light");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- src/__tests__/App.test.tsx`
Expected: FAIL — no wordmark link, because `App` does not render the shell yet.

- [ ] **Step 3: Rewrite `App.tsx`**

```tsx
import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { Toaster } from "sonner";
import { AppRoutes } from "@/routes";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { createQueryClient } from "@/lib/query-client";
import { useTheme } from "@/lib/theme/useTheme";

const queryClient = createQueryClient();

/**
 * ThemedToaster keeps sonner in step with the app theme. It reads context here,
 * rather than in App, so a theme change does not re-render App itself. The
 * resolved theme is passed, never the literal "system".
 */
function ThemedToaster() {
  const { resolved } = useTheme();
  return <Toaster richColors position="top-right" theme={resolved} />;
}

export function App() {
  return (
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
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npm test -- src/__tests__/App.test.tsx`
Expected: 3 passing.

If the `data-theme` assertion fails, inspect the toaster element's actual attributes and assert on whichever attribute `sonner@2.0.8` uses to reflect the `theme` prop. The requirement is that the resolved theme reaches `Toaster`; keep that, fix the selector. Do not assert on `"system"` — that value must never be passed.

- [ ] **Step 5: Full suite, lint, format, build**

Run: `npm test && npm run lint && npm run format:check && npm run build`
Expected: clean. The page tests render pages directly through `renderWithProviders`, not through `App`, so they are unaffected by the shell.

- [ ] **Step 6: Commit**

```bash
git add src/App.tsx src/__tests__/App.test.tsx
git commit -m "feat(frontend): mount the theme provider, shell, and themed toaster" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Theme the diff view

`@pierre/diffs@1.4.1` ships its own matched pair — `DEFAULT_THEMES = { dark: "pierre-dark", light: "pierre-light" }` (`dist/constants.js:28-31`) — pre-registered from `@pierre/theming`. Those are the themes the library's own gutters, hunk separators, and addition/deletion backgrounds were designed against, and `pierre-light` is what the review page renders today, so light mode is visually unchanged while dark gets a matched counterpart. Passing them explicitly rather than relying on the default makes the choice assertable.

`themeType` takes the **resolved** theme, never `"system"`, for two independent reasons: `"system"` would make the diff follow the OS and diverge from an explicitly pinned app theme, and `dist/utils/cssWrappers.js:20-24` only emits `:host { color-scheme: … }` into the diff's shadow root when `themeType !== "system"` — that declaration is what gives the diff's internal scroll containers correct native scrollbars.

**Do not add the theme to `PatchDiff`'s `key`.** `dist/react/utils/useFileDiffInstance.js` compares incoming options with `areOptionsEqual` — a by-value comparison that special-cases `theme` — and calls `instance.setOptions(...)` with a forced re-render when they differ. FR-8.5 (re-theme in place) is satisfied by the existing prop flow. Keying on the theme would discard the parsed diff and the highlighter cache on every toggle.

**Files:**
- Modify: `src/components/features/review/FileDiff.tsx`
- Modify: `src/components/features/review/__tests__/FileDiff.test.tsx`

**Interfaces:**
- Consumes: `useTheme`.
- Produces: no API change to `FileDiff` — still `{ file: ReviewFileDiff }`.

- [ ] **Step 1: Write the failing test**

Add to `src/components/features/review/__tests__/FileDiff.test.tsx`. Keep the existing `NOTE:` comment, the `diffFile` helper, and both existing `it` blocks exactly as they are; add the mock at the top of the file and this new `describe` below them.

The mock captures the props Converge passes. It must be hoisted, so declare the capture array with `vi.hoisted`.

```tsx
import userEvent from "@testing-library/user-event";
import { ThemeToggle } from "@/components/theme/ThemeToggle";
import { setSystemDark } from "@/test/matchMedia";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";

const captured = vi.hoisted(() => ({ options: [] as Record<string, unknown>[] }));

vi.mock("@pierre/diffs/react", () => ({
  PatchDiff: (props: { options: Record<string, unknown> }) => {
    captured.options.push(props.options);
    return <div data-testid="patch-diff" />;
  },
}));

beforeEach(() => {
  captured.options.length = 0;
});

function lastOptions() {
  return captured.options[captured.options.length - 1];
}

describe("FileDiff theming", () => {
  it("forwards the resolved light theme into PatchDiff options", () => {
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions().themeType).toBe("light");
    expect(lastOptions().theme).toEqual({ light: "pierre-light", dark: "pierre-dark" });
  });

  it("forwards dark when the preference is dark", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions().themeType).toBe("dark");
  });

  it("forwards the resolved theme under system, never the literal system", () => {
    setSystemDark(true);
    localStorage.setItem(THEME_STORAGE_KEY, "system");
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions().themeType).toBe("dark");
    for (const options of captured.options) {
      expect(options.themeType).not.toBe("system");
    }
  });

  it("keeps the existing diff options alongside the theme", () => {
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions()).toMatchObject({
      diffStyle: "unified",
      expandUnchanged: true,
      collapsedContextThreshold: 8,
      overflow: "scroll",
    });
  });

  it("re-renders the diff with the new theme when the theme changes in place", async () => {
    const user = userEvent.setup();
    renderWithProviders(
      <>
        <ThemeToggle />
        <FileDiff file={diffFile()} />
      </>,
    );
    expect(lastOptions().themeType).toBe("light");

    await user.click(screen.getByRole("button", { name: /change theme/i }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));

    expect(lastOptions().themeType).toBe("dark");
  });
});
```

Add `beforeEach` and `vi` to the `vitest` import on the file's first lines:

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- src/components/features/review/__tests__/FileDiff.test.tsx`
Expected: the two original tests pass; the new theming tests FAIL with `expected undefined to be "light"`.

- [ ] **Step 3: Forward the theme**

In `src/components/features/review/FileDiff.tsx`, add the import:

```tsx
import { useTheme } from "@/lib/theme/useTheme";
```

read the resolved theme at the top of the component body, immediately after the existing destructure:

```tsx
  const { binary, truncated, diff, path } = file.attributes;
  const { resolved } = useTheme();
```

and extend the `options` object, leaving `key={path}` and every existing option as-is:

```tsx
        options={{
          diffStyle: "unified",
          expandUnchanged: true,
          collapsedContextThreshold: 8,
          overflow: "scroll",
          // The resolved theme, never "system": "system" would make the diff
          // follow the OS instead of the app, and suppresses the shadow-root
          // color-scheme declaration that gives the diff correct scrollbars.
          themeType: resolved,
          theme: { light: "pierre-light", dark: "pierre-dark" },
        }}
```

Note: the `useTheme()` call must sit above the `if (binary)` early return, or the hook order changes between renders.

- [ ] **Step 4: Run the test to verify it passes**

Run: `npm test -- src/components/features/review/__tests__/FileDiff.test.tsx`
Expected: all passing.

- [ ] **Step 5: Full suite, lint, format, build**

Run: `npm test && npm run lint && npm run format:check && npm run build`
Expected: clean. The mock is file-scoped, so `ReviewPage.test.tsx` still renders the real `PatchDiff`.

- [ ] **Step 6: Commit**

```bash
git add src/components/features/review/FileDiff.tsx \
  src/components/features/review/__tests__/FileDiff.test.tsx
git commit -m "feat(frontend): theme the diff view with pierre light and dark" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Dark-mode audit regression check, manual verification, and full CI

The design already ran the §4.9 audit: zero hardcoded palette classes, zero hex/`rgb()` literals, zero inline `style` colors, and zero `dark:` usage outside the lint-ignored `src/components/ui/**`. Every component already draws from semantic tokens, so FR-9.1 and FR-9.2 require no conversions. This task re-runs those greps as a regression check against the code this plan added, records the result, and closes out with the full CI suite plus the checks a test cannot make.

**Files:**
- Create: `docs/tasks/task-002-theme-mode-selection/audit-dark-mode.md`

**Interfaces:**
- Consumes: everything above.
- Produces: the FR-9.4 record.

- [ ] **Step 1: Re-run the three audit greps**

From `apps/frontend`:

```bash
grep -rnE '(bg|text|border|ring|fill|stroke|divide|shadow)-(white|black|slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)' \
  src --include='*.tsx' --include='*.ts' | grep -v '^src/components/ui/'

grep -rnE '#[0-9a-fA-F]{3,8}|rgba?\(|style=\{\{' src --include='*.tsx' --include='*.ts' \
  | grep -v '^src/components/ui/'

grep -rn 'dark:' src/components/common src/components/features src/components/layout \
  src/components/theme src/pages
```

Expected: the first two return only the `#421` / `#435` PR numbers in test fixtures; the third returns nothing. Anything else is a finding — fix it by converting to the appropriate semantic token (`bg-background`, `text-muted-foreground`, `border-border`, …) before continuing, and record it in Step 2.

- [ ] **Step 2: Record the audit**

Create `docs/tasks/task-002-theme-mode-selection/audit-dark-mode.md` with: the three commands verbatim, their output, the date, and one of "no findings" or the list of findings with the file:line and the token each was converted to. Also note the two token-value observations carried from the design as follow-up candidates rather than in-scope work, since repaletting is a stated non-goal: `--destructive` on dark (`oklch(0.704 0.191 22.216)` on `oklch(0.145 0 0)`) and `--muted-foreground` (`oklch(0.708 0 0)`) on `--muted` (`oklch(0.269 0 0)`). Use repo-relative paths only — no absolute home paths.

- [ ] **Step 3: Verify by hand in a browser**

Run `npm run dev` and walk the acceptance criteria that no unit test can reach. Check each, and note any failure with the page and element:

- Dark, Light, and System each take effect immediately on all three routes, with no reload.
- With System selected, flipping the OS theme updates the UI live; with Light or Dark pinned, it does not.
- The selection survives a reload and a browser restart.
- With `Dark` stored, a hard reload shows **no white flash** at any point. This is the inline script's whole purpose; if it flashes, check that the script is still the first child of `<head>` and has no `type`/`defer`/`async`.
- Setting `localStorage["converge.theme"] = "twilight"` and reloading loads cleanly as System.
- All three pages are legible in dark mode: no light-on-light or dark-on-dark text, no stray white panels.
- The review page's diffs render in `pierre-dark` with additions, deletions, and syntax highlighting all legible; toggling the theme with the page open re-themes the diff in place without navigating away. A frame or two of the old theme during the switch is expected — Shiki loads themes on demand inside the library.
- A toast (trigger one by forcing an API error) matches the active theme in both modes.
- `ReviewErrorPanel` and the `destructive` `Badge` variant stay distinguishable in dark mode. Measure their text contrast with browser devtools and confirm ≥ 4.5:1; record the numbers in `audit-dark-mode.md`.

- [ ] **Step 4: Run the frontend gate**

```bash
npm ci && npm run lint && npm run format:check && npm test && npm run build
```

Expected: all clean. `npm test` should report the original 97 plus the tests added by Tasks 1–9.

- [ ] **Step 5: Run the repository gate**

From the repository root:

```bash
make lint && make test && make test-integration && make build && make docker-build
```

Expected: all clean. No Go source changed; `make build` and `make docker-build` exercise the regenerated embedded `internal/ui/dist` bundle. Give these an explicit generous timeout — `make docker-build` is slow.

- [ ] **Step 6: Commit**

```bash
git add docs/tasks/task-002-theme-mode-selection/audit-dark-mode.md
git commit -m "docs(task-002): record the dark-mode audit and verification results" \
  -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Code review before the PR**

Per `CLAUDE.md`, run the review step before opening a PR — do not skip it even though the plan is complete. Use `superpowers:requesting-code-review` (it dispatches `plan-adherence-reviewer` plus `frontend-guidelines-reviewer`, since only frontend files changed). Findings land in `docs/tasks/task-002-theme-mode-selection/audit.md`. Address Critical and Important findings before the PR.

---

## Requirement coverage

| Requirement | Task |
| --- | --- |
| FR-1.1, FR-1.2 | 2 (`types.ts`) |
| FR-1.3, FR-1.4 | 2 (`resolveTheme`, `prefersDark`) |
| FR-1.5 | 3 (context exposes both), 6 (picker reads each) |
| FR-2.1, FR-2.2, FR-2.4 | 2 (`applyTheme`), 5 (`color-scheme` split) |
| FR-2.3 | 3 (`useLayoutEffect`) |
| FR-3.1 – FR-3.5 | 2 (`storage.ts`), 3 (`setPreference` persists the literal) |
| FR-4.1, FR-4.2, FR-4.4 | 5 (boot script + paired comments + drift test) |
| FR-4.3 | 2 (idempotent `applyTheme`), 3 (`useLayoutEffect`) |
| FR-5.1 – FR-5.3 | 3 (always-on listener, derived resolution, effect cleanup) |
| FR-6.1 – FR-6.5 | 6 (`ThemeToggle`) |
| FR-6.6 | 6 (vendored `dropdown-menu.tsx`) |
| FR-7.1, FR-7.2, FR-7.4, FR-7.5 | 7 (`AppShell`), 8 (wrapping `AppRoutes`) |
| FR-7.3 | 4 (routes test unchanged but for the wrapper), 7 (wrapper, not a layout route) |
| FR-8.1 | 8 (`ThemedToaster`) |
| FR-8.2 – FR-8.5 | 9 (`FileDiff` options) |
| FR-9.1 – FR-9.4 | 10 (regression greps + recorded audit) |
| Acceptance: no flash, WCAG AA contrast | 10 Step 3 (manual) |
| Acceptance: every CI command clean | 10 Steps 4–5 |
| Acceptance: code review before PR | 10 Step 7 |
