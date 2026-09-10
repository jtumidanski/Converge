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
