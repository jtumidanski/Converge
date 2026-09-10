import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ReviewErrorPanel } from "@/components/features/review/ReviewErrorPanel";
import type { Review } from "@/types/models/review";

function conflictedReview(): Review {
  return {
    type: "reviews",
    id: "7f14b2c8",
    attributes: {
      status: "CONFLICTED",
      stage: null,
      provider: "gitlab-work",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: "9f21a43".padEnd(40, "0"),
      headSha: null,
      baseDescription: "Immediately before #421",
      changes: [421, 435],
      included: [],
      totals: null,
      error: {
        code: "CONFLICT",
        message:
          "#435 conflicts while being applied. It may depend on work that is not part of this review.",
        change: 435,
        commit: "c0ffee".padEnd(40, "0"),
        conflictingFiles: ["src/field/FieldService.java"],
        appliedChanges: [421],
        possibleDependency: true,
        diagnostics: {
          workspacePath: "/data/workspaces/7f14b2c8/repo",
          branch: "review/7f14b2c8",
          strategy: "squash",
          sourceSha: "abc".padEnd(40, "0"),
        },
      },
      createdAt: "2026-09-01T12:00:00Z",
      updatedAt: "2026-09-01T12:01:00Z",
      expiresAt: "2026-09-02T12:00:00Z",
    },
  };
}

describe("ReviewErrorPanel", () => {
  it("shows the message, offending change and conflicting files", () => {
    render(<ReviewErrorPanel review={conflictedReview()} onDiscard={vi.fn()} discarding={false} />);
    expect(screen.getByText(/#435 conflicts/i)).toBeInTheDocument();
    expect(screen.getByText("src/field/FieldService.java")).toBeInTheDocument();
    expect(screen.getByText(/#421/)).toBeInTheDocument();
  });

  it("hides git vocabulary until Diagnostics is expanded", async () => {
    render(<ReviewErrorPanel review={conflictedReview()} onDiscard={vi.fn()} discarding={false} />);
    expect(screen.queryByText(/review\/7f14b2c8/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /diagnostics/i }));
    expect(await screen.findByText(/review\/7f14b2c8/)).toBeInTheDocument();
    expect(screen.getByText("CONFLICT")).toBeInTheDocument();
    expect(screen.getByText(/\/data\/workspaces\/7f14b2c8\/repo/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /workspace/i })).not.toBeInTheDocument();
  });

  it("calls onDiscard", async () => {
    const onDiscard = vi.fn();
    render(
      <ReviewErrorPanel review={conflictedReview()} onDiscard={onDiscard} discarding={false} />,
    );
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
  });
});
