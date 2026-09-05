import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ReviewStatus } from "@/components/features/review/ReviewStatus";

describe("ReviewStatus", () => {
  it("renders a human label for each stage", () => {
    const cases: Array<[string | null, RegExp]> = [
      ["resolving", /resolving/i],
      ["updating-repository", /updating repository/i],
      ["creating-workspace", /preparing review/i],
      ["applying:435", /applying #435/i],
      ["diffing", /combined diff/i],
      [null, /building/i],
    ];
    for (const [stage, expected] of cases) {
      const { unmount } = render(<ReviewStatus stage={stage} />);
      expect(screen.getByText(expected)).toBeInTheDocument();
      unmount();
    }
  });

  it("never shows git vocabulary", () => {
    const { container } = render(<ReviewStatus stage="creating-workspace" />);
    expect(container.textContent?.toLowerCase()).not.toContain("worktree");
    expect(container.textContent?.toLowerCase()).not.toContain("cherry-pick");
  });
});
