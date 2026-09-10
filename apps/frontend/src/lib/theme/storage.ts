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
