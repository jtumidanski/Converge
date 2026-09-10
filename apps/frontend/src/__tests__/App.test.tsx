import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";

// sonner resolves a "system" theme prop via the same matchMedia stub the app's
// own ThemeProvider uses, so asserting on sonner's rendered DOM output cannot
// distinguish "we passed the resolved theme" from "we passed the literal
// string 'system' and sonner resolved it itself". Mock the module and capture
// the prop Toaster actually receives instead (FR-8.1).
const { capturedThemes } = vi.hoisted(() => ({ capturedThemes: [] as string[] }));

vi.mock("sonner", () => ({
  Toaster: (props: { theme?: string }) => {
    capturedThemes.push(props.theme ?? "");
    return null;
  },
  toast: vi.fn(),
}));

beforeEach(() => {
  window.history.pushState({}, "", "/no-such-page");
  capturedThemes.length = 0;
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

  it.each([
    ["light", "light"],
    ["dark", "dark"],
    ["system", "light"],
  ] as const)(
    'passes Toaster the resolved theme for stored preference "%s", never the literal "system"',
    (stored, expectedResolved) => {
      localStorage.setItem(THEME_STORAGE_KEY, stored);
      render(<App />);
      expect(capturedThemes.length).toBeGreaterThan(0);
      const lastTheme = capturedThemes[capturedThemes.length - 1];
      expect(lastTheme).toBe(expectedResolved);
      expect(lastTheme).not.toBe("system");
    },
  );
});
