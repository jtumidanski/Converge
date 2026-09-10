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
