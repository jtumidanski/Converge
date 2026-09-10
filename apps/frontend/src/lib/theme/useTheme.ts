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
