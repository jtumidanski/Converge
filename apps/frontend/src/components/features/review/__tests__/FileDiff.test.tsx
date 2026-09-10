import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FileDiff } from "@/components/features/review/FileDiff";
import { ThemeToggle } from "@/components/theme/ThemeToggle";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";
import { setSystemDark } from "@/test/matchMedia";
import { renderWithProviders } from "@/test/render";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

const captured = vi.hoisted(() => ({ options: [] as Record<string, unknown>[] }));

vi.mock("@pierre/diffs/react", () => ({
  PatchDiff: (props: { options: Record<string, unknown> }) => {
    captured.options.push(props.options);
    return <div data-testid="patch-diff" />;
  },
}));

beforeEach(() => {
  captured.options.length = 0;
});

function lastOptions(): Record<string, unknown> {
  const options = captured.options[captured.options.length - 1];
  if (!options) throw new Error("PatchDiff was never rendered with options");
  return options;
}

function diffFile(overrides: Partial<ReviewFileDiff["attributes"]> = {}): ReviewFileDiff {
  return {
    type: "review-file-diffs",
    id: overrides.path ?? "foo.txt",
    attributes: {
      path: "foo.txt",
      previousPath: "",
      status: "modified",
      additions: 1,
      deletions: 1,
      binary: false,
      truncated: false,
      diff: [
        "diff --git a/foo.txt b/foo.txt",
        "index e69de29..0cfbf08 100644",
        "--- a/foo.txt",
        "+++ b/foo.txt",
        "@@ -1 +1 @@",
        "-old",
        "+new",
        "",
      ].join("\n"),
      ...overrides,
    },
  };
}

describe("FileDiff", () => {
  // NOTE: `@pierre/diffs/react` is mocked at the top of this file, so these
  // tests assert on what FileDiff *passes* to PatchDiff (via `lastOptions()`),
  // never on the patch markup PatchDiff would render. That rendering lives in a
  // <diffs-container> custom element's shadow DOM, which light-DOM Testing
  // Library queries cannot see by design; asserting on it is the vendor's job,
  // not ours.

  it("shows a binary file notice instead of the patch", () => {
    renderWithProviders(<FileDiff file={diffFile({ binary: true })} />);
    expect(screen.getByText(/binary file changed/i)).toBeInTheDocument();
  });

  it("shows a truncation notice for a truncated diff", () => {
    renderWithProviders(<FileDiff file={diffFile({ truncated: true })} />);
    expect(screen.getByText(/too large to display in full/i)).toBeInTheDocument();
  });
});

describe("FileDiff theming", () => {
  it("forwards the resolved light theme into PatchDiff options", () => {
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions().themeType).toBe("light");
    expect(lastOptions().theme).toEqual({ light: "pierre-light", dark: "pierre-dark" });
  });

  it("forwards dark when the preference is dark", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions().themeType).toBe("dark");
  });

  it("forwards the resolved theme under system, never the literal system", () => {
    setSystemDark(true);
    localStorage.setItem(THEME_STORAGE_KEY, "system");
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions().themeType).toBe("dark");
    for (const options of captured.options) {
      expect(options.themeType).not.toBe("system");
    }
  });

  it("keeps the existing diff options alongside the theme", () => {
    renderWithProviders(<FileDiff file={diffFile()} />);
    expect(lastOptions()).toMatchObject({
      diffStyle: "unified",
      expandUnchanged: true,
      collapsedContextThreshold: 8,
      overflow: "scroll",
    });
  });

  it("re-renders the diff with the new theme when the theme changes in place", async () => {
    const user = userEvent.setup();
    renderWithProviders(
      <>
        <ThemeToggle />
        <FileDiff file={diffFile()} />
      </>,
    );
    expect(lastOptions().themeType).toBe("light");

    await user.click(screen.getByRole("button", { name: /change theme/i }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));

    expect(lastOptions().themeType).toBe("dark");
  });
});
