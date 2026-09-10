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
