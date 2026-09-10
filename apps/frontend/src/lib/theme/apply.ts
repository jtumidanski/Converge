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
