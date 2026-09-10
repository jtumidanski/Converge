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
