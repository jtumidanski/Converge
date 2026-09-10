import { readFileSync } from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { beforeEach, describe, expect, it } from "vitest";
import { applyTheme, resolveTheme } from "@/lib/theme/apply";
import type { ThemePreference } from "@/lib/theme/types";
import { DEFAULT_PREFERENCE, THEME_STORAGE_KEY, isThemePreference } from "@/lib/theme/types";

/**
 * The pre-paint boot script in index.html is a hand-inlined copy of
 * resolveTheme + applyTheme (an inline classic script cannot import). These
 * tests EXECUTE that script in a node:vm sandbox and compare its DOM writes
 * against what the TypeScript core produces for the same inputs, so the two
 * copies cannot drift silently.
 */

const html = readFileSync(path.resolve(import.meta.dirname, "../../index.html"), "utf8");

const DARK_QUERY = "(prefers-color-scheme: dark)";

function bootScriptSource(): string {
  const open = html.indexOf("<script>");
  const close = html.indexOf("</script>", open);
  if (open === -1 || close === -1) {
    throw new Error("no inline classic <script> found in index.html");
  }
  return html.slice(open + "<script>".length, close);
}

interface BootOutcome {
  /** hasDarkClass is whether the script left the `dark` token on the root. */
  hasDarkClass: boolean;
  colorScheme: string;
  /** otherClasses proves the script only touched the `dark` token. */
  otherClasses: string[];
}

interface BootInputs {
  stored?: string | null;
  storageThrows?: boolean;
  systemIsDark?: boolean;
  noMatchMedia?: boolean;
  matchMediaThrows?: boolean;
}

/** runBootScript executes the inline script against stubbed browser globals. */
function runBootScript(inputs: BootInputs): BootOutcome {
  const classes = new Set<string>(["preexisting"]);
  const style = { colorScheme: "" };
  const documentElement = {
    classList: {
      toggle(token: string, force: boolean) {
        if (force) classes.add(token);
        else classes.delete(token);
      },
    },
    style,
  };

  const localStorage = {
    getItem(key: string): string | null {
      if (inputs.storageThrows) throw new Error("storage is blocked");
      return key === THEME_STORAGE_KEY ? (inputs.stored ?? null) : null;
    },
  };

  const matchMedia = inputs.noMatchMedia
    ? undefined
    : (query: string) => {
        if (inputs.matchMediaThrows) throw new Error("matchMedia is unavailable");
        return { matches: query === DARK_QUERY && inputs.systemIsDark === true };
      };

  const sandbox = {
    window: { matchMedia },
    localStorage,
    document: { documentElement },
  };

  vm.runInNewContext(bootScriptSource(), vm.createContext(sandbox), {
    filename: "index.html#boot",
  });

  return {
    hasDarkClass: classes.has("dark"),
    colorScheme: style.colorScheme,
    otherClasses: [...classes].filter((token) => token !== "dark"),
  };
}

/** coreOutcome is what resolveTheme + applyTheme produce for the same inputs. */
function coreOutcome(stored: string | null, systemIsDark: boolean): BootOutcome {
  const preference: ThemePreference = isThemePreference(stored) ? stored : DEFAULT_PREFERENCE;
  applyTheme(resolveTheme(preference, systemIsDark));
  const root = document.documentElement;
  return {
    hasDarkClass: root.classList.contains("dark"),
    colorScheme: root.style.colorScheme,
    otherClasses: [],
  };
}

const STORED_VALUES: (string | null)[] = [null, "light", "dark", "system", "twilight"];

describe("pre-paint boot script, executed", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("dark");
    document.documentElement.style.colorScheme = "";
  });

  it.each([
    { stored: null, systemIsDark: false, dark: false },
    { stored: null, systemIsDark: true, dark: true },
    { stored: "light", systemIsDark: false, dark: false },
    { stored: "light", systemIsDark: true, dark: false },
    { stored: "dark", systemIsDark: false, dark: true },
    { stored: "dark", systemIsDark: true, dark: true },
    { stored: "system", systemIsDark: false, dark: false },
    { stored: "system", systemIsDark: true, dark: true },
    { stored: "twilight", systemIsDark: false, dark: false },
    { stored: "twilight", systemIsDark: true, dark: true },
  ])(
    "stored=$stored with OS dark=$systemIsDark applies dark=$dark",
    ({ stored, systemIsDark, dark }) => {
      const outcome = runBootScript({ stored, systemIsDark });
      expect(outcome.hasDarkClass).toBe(dark);
      expect(outcome.colorScheme).toBe(dark ? "dark" : "light");
    },
  );

  it.each(
    STORED_VALUES.flatMap((stored) =>
      [false, true].map((systemIsDark) => ({ stored, systemIsDark })),
    ),
  )("matches resolveTheme+applyTheme for stored=$stored, OS dark=$systemIsDark", (inputs) => {
    const boot = runBootScript(inputs);
    const core = coreOutcome(inputs.stored, inputs.systemIsDark);
    expect(boot.hasDarkClass).toBe(core.hasDarkClass);
    expect(boot.colorScheme).toBe(core.colorScheme);
  });

  it("leaves unrelated root classes alone", () => {
    expect(runBootScript({ stored: "dark" }).otherClasses).toEqual(["preexisting"]);
    expect(runBootScript({ stored: "light" }).otherClasses).toEqual(["preexisting"]);
  });

  it("still applies the OS preference when localStorage throws", () => {
    const outcome = runBootScript({ storageThrows: true, systemIsDark: true });
    expect(outcome.hasDarkClass).toBe(true);
    expect(outcome.colorScheme).toBe("dark");
  });

  it("applies light when localStorage throws and the OS prefers light", () => {
    const outcome = runBootScript({ storageThrows: true, systemIsDark: false });
    expect(outcome.hasDarkClass).toBe(false);
    expect(outcome.colorScheme).toBe("light");
  });

  it("falls back to light when matchMedia is absent", () => {
    const outcome = runBootScript({ stored: "system", noMatchMedia: true, systemIsDark: true });
    expect(outcome.hasDarkClass).toBe(false);
    expect(outcome.colorScheme).toBe("light");
  });

  it("does not throw when matchMedia itself throws", () => {
    const outcome = runBootScript({ stored: "system", matchMediaThrows: true });
    expect(outcome.hasDarkClass).toBe(false);
    expect(outcome.colorScheme).toBe("light");
  });

  it("honours a stored dark preference even when matchMedia is unusable", () => {
    const outcome = runBootScript({ stored: "dark", matchMediaThrows: true });
    expect(outcome.hasDarkClass).toBe(true);
    expect(outcome.colorScheme).toBe("dark");
  });
});
