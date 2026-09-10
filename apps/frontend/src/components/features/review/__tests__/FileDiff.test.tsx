import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { FileDiff } from "@/components/features/review/FileDiff";
import { renderWithProviders } from "@/test/render";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

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
  // NOTE: there is intentionally no test here that asserts on @pierre/diffs'
  // rendered patch output. See the FileDiff.tsx comment and the task report:
  // PatchDiff renders its content inside a <diffs-container> custom element's
  // shadow DOM (not reflected by `.textContent` or Testing Library queries,
  // by design — light-DOM APIs never see shadow-DOM content) and calls
  // `ResizeObserver`, which jsdom does not implement. Polyfilling
  // ResizeObserver would only prove the polyfill runs, not that the diff
  // renders, so no shim was added.

  it("shows a binary file notice instead of the patch", () => {
    renderWithProviders(<FileDiff file={diffFile({ binary: true })} />);
    expect(screen.getByText(/binary file changed/i)).toBeInTheDocument();
  });

  it("shows a truncation notice for a truncated diff", () => {
    renderWithProviders(<FileDiff file={diffFile({ truncated: true })} />);
    expect(screen.getByText(/too large to display in full/i)).toBeInTheDocument();
  });
});
