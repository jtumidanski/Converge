import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ResumeReviewList } from "@/components/features/reviews/ResumeReviewList";
import type { Review, ReviewAttributes } from "@/types/models/review";

function makeReview(id: string, overrides: Partial<ReviewAttributes> = {}): Review {
  return {
    type: "reviews",
    id,
    attributes: {
      status: "READY",
      stage: null,
      provider: "gitlab-work",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: "a".repeat(40),
      headSha: "b".repeat(40),
      baseDescription: "Immediately before #12",
      changes: [12, 14, 15],
      included: [],
      totals: { files: 5, additions: 84, deletions: 12 },
      error: null,
      createdAt: new Date(Date.now() - 12 * 60_000).toISOString(),
      updatedAt: new Date(Date.now() - 60_000).toISOString(),
      expiresAt: new Date(Date.now() + 5 * 3_600_000).toISOString(),
      ...overrides,
    },
  };
}

function renderList(props: Partial<Parameters<typeof ResumeReviewList>[0]> = {}) {
  const onResume = vi.fn();
  const onDiscard = vi.fn();
  const onRetry = vi.fn();
  render(
    <ResumeReviewList
      reviews={[makeReview("r1")]}
      loading={false}
      onRetry={onRetry}
      onResume={onResume}
      onDiscard={onDiscard}
      {...props}
    />,
  );
  return { onResume, onDiscard, onRetry };
}

describe("ResumeReviewList", () => {
  it("renders a heading and a count of the listed reviews", () => {
    renderList({ reviews: [makeReview("r1"), makeReview("r2")] });
    expect(screen.getByRole("heading", { name: /resume a review/i })).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("names the section landmark after its heading and gives the count context", () => {
    renderList({ reviews: [makeReview("r1"), makeReview("r2")] });
    expect(screen.getByRole("region", { name: /resume a review/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/2 reviews in progress/i)).toBeInTheDocument();
  });

  it("renders a READY row with repository, provider, changes and totals", () => {
    renderList();
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText("gitlab-work")).toBeInTheDocument();
    expect(screen.getByText(/#12, #14, #15/)).toBeInTheDocument();
    expect(screen.getByText(/5 files/)).toBeInTheDocument();
    expect(screen.getByText(/\+84/)).toBeInTheDocument();
  });

  it("omits totals rather than rendering zeros when totals is null", () => {
    renderList({ reviews: [makeReview("r1", { totals: null })] });
    expect(screen.queryByText(/files/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\+0/)).not.toBeInTheDocument();
  });

  it("renders a CREATING row with the shared stage label", () => {
    renderList({
      reviews: [makeReview("r1", { status: "CREATING", stage: "resolving", totals: null })],
    });
    expect(screen.getByText("Building")).toBeInTheDocument();
    expect(screen.getByText("Resolving PRs/MRs")).toBeInTheDocument();
  });

  it("renders a CONFLICTED row with a one-line error summary naming the change", () => {
    renderList({
      reviews: [
        makeReview("r1", {
          status: "CONFLICTED",
          totals: null,
          error: { code: "CONFLICT", message: "A long panel-sized sentence.", change: 14 },
        }),
      ],
    });
    expect(screen.getByText("Conflict")).toBeInTheDocument();
    expect(screen.getByText(/CONFLICT on #14/)).toBeInTheDocument();
    expect(screen.queryByText(/panel-sized/)).not.toBeInTheDocument();
  });

  it("renders a FAILED row without a change number", () => {
    renderList({
      reviews: [
        makeReview("r1", {
          status: "FAILED",
          totals: null,
          error: { code: "GIT_FAILURE", message: "…" },
        }),
      ],
    });
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText(/GIT_FAILURE/)).toBeInTheDocument();
  });

  it("renders an unrecognised status as a neutral badge rather than throwing", () => {
    const review = makeReview("r1", { totals: null });
    // Deliberately outside the union: the server may add a status the client
    // does not know, and a stale cached list may hold FINISHED/EXPIRED.
    (review.attributes as { status: string }).status = "SOMETHING_NEW";
    renderList({ reviews: [review] });
    expect(screen.getByText("SOMETHING_NEW")).toBeInTheDocument();
  });

  it("marks a near expiry with a warning role", () => {
    renderList({
      reviews: [makeReview("r1", { expiresAt: new Date(Date.now() + 47 * 60_000).toISOString() })],
    });
    expect(screen.getByRole("status")).toHaveTextContent(/expires in/i);
  });

  it("keeps the expiry live region present before the near-expiry threshold, so a later crossing is announced", () => {
    // The region must exist unconditionally rather than being created only once
    // nearExpiry becomes true: a live region that appears already populated is
    // never announced by assistive tech.
    renderList({
      reviews: [
        makeReview("r1", { expiresAt: new Date(Date.now() + 5 * 3_600_000).toISOString() }),
      ],
    });
    const region = screen.getByRole("status");
    expect(region).toHaveTextContent(/expires in/i);
    expect(region.className).not.toContain("text-destructive");
  });

  it("renders an already-past expiry as expired, not a negative duration", () => {
    renderList({
      reviews: [makeReview("r1", { expiresAt: new Date(Date.now() - 60_000).toISOString() })],
    });
    expect(screen.getByText(/expired/)).toBeInTheDocument();
  });

  it("renders skeletons and no empty state while loading", () => {
    renderList({ reviews: [], loading: true });
    expect(screen.queryByText(/no reviews in progress/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("renders the empty state once the query resolves with no reviews", () => {
    renderList({ reviews: [], loading: false });
    expect(screen.getByText(/no reviews in progress/i)).toBeInTheDocument();
  });

  it("renders a retryable error banner and calls onRetry", async () => {
    const { onRetry } = renderList({ reviews: [], error: new Error("boom") });
    expect(screen.getByRole("alert")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("calls onResume with the review id", async () => {
    const { onResume } = renderList();
    await userEvent.click(screen.getByRole("button", { name: /resume review of atlas\/server/i }));
    expect(onResume).toHaveBeenCalledWith("r1");
  });

  it("does not discard until the confirmation is accepted", async () => {
    const { onDiscard } = renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    expect(onDiscard).not.toHaveBeenCalled();
    expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(onDiscard).not.toHaveBeenCalled();
    expect(screen.queryByText(/cannot be undone/i)).not.toBeInTheDocument();
  });

  it("focuses Cancel, not Discard, when the two-step confirmation opens", async () => {
    // A destructive action must not be one keypress away: this protects the
    // controller ruling that moved autoFocus off the destructive button.
    renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    expect(screen.getByRole("button", { name: /^cancel$/i })).toHaveFocus();
  });

  it("returns focus to the Discard trigger once the confirmation is cancelled", async () => {
    renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(screen.getByRole("button", { name: /discard review of atlas\/server/i })).toHaveFocus();
  });

  it("describes the confirmation buttons with the destructive question", async () => {
    renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    const question = screen.getByText(/cannot be undone/i);
    expect(screen.getByRole("button", { name: /^cancel$/i })).toHaveAccessibleDescription(
      question.textContent ?? "",
    );
    expect(screen.getByRole("button", { name: /^discard$/i })).toHaveAccessibleDescription(
      question.textContent ?? "",
    );
  });

  it("calls onDiscard once when the confirmation is accepted", async () => {
    const { onDiscard } = renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    await userEvent.click(screen.getByRole("button", { name: /^discard$/i }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(onDiscard).toHaveBeenCalledWith("r1");
  });

  it("disables only the pending row's controls", () => {
    renderList({
      reviews: [makeReview("r1"), makeReview("r2", { repository: "web/ui" })],
      pendingId: "r1",
    });
    expect(screen.getByRole("button", { name: /resume review of atlas\/server/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /resume review of web\/ui/i })).toBeEnabled();
  });

  it("marks the pending row's discard control as busy for assistive tech", () => {
    renderList({
      reviews: [makeReview("r1")],
      pendingId: "r1",
    });
    expect(
      screen.getByRole("button", { name: /discard review of atlas\/server/i }),
    ).toHaveAttribute("aria-busy", "true");
  });
});
