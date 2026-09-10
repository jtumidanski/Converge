import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_PREFERENCE, THEME_STORAGE_KEY, isThemePreference } from "@/lib/theme/types";
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
