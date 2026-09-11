import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewsTable } from "@/components/features/reviews/ReviewsTable";
import { toggleViewed } from "@/lib/storage/viewed";
import type { Review, ReviewStatus } from "@/types/models/review";

function review(
  id: string,
  status: ReviewStatus,
  overrides: Partial<Review["attributes"]> = {},
): Review {
  return {
    type: "reviews",
    id,
    attributes: {
      status,
      stage: null,
      provider: "gl",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: null,
      headSha: null,
      baseDescription: "",
      changes: [421, 430],
      included: [],
      totals: { files: 4, additions: 120, deletions: 30 },
      error: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      expiresAt: "2999-01-01T00:00:00Z",
      ...overrides,
    },
  };
}

function renderTable(props: Partial<React.ComponentProps<typeof ReviewsTable>> = {}) {
  const onOpen = vi.fn();
  const onDiscard = vi.fn();
  const onNewReview = vi.fn();
  render(
    <MemoryRouter>
      <ReviewsTable
        reviews={[review("r1", "READY")]}
        loading={false}
        onOpen={onOpen}
        onDiscard={onDiscard}
        onNewReview={onNewReview}
        {...props}
      />
    </MemoryRouter>,
  );
  return { onOpen, onDiscard, onNewReview };
}

afterEach(() => localStorage.clear());

describe("ReviewsTable", () => {
  it("exposes an accessible column header for every column", () => {
    renderTable();
    const headers = screen.getAllByRole("columnheader");
    expect(headers).toHaveLength(5);
    expect(headers.map((header) => header.textContent)).toEqual([
      "Status",
      "Repository",
      "Progress",
      "Expires",
      "Actions",
    ]);
  });

  it("shows the repository, change numbers, and totals", () => {
    renderTable();
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
    const row = screen.getByRole("row", { name: /atlas\/server/ });
    expect(row).toHaveTextContent("#421");
    expect(row).toHaveTextContent("#430");
    expect(row).toHaveTextContent("4 files");
    expect(row).toHaveTextContent("+120");
    expect(row).toHaveTextContent("−30");
  });

  it("reports viewed progress from browser state", () => {
    toggleViewed("r1", "a.ts");
    toggleViewed("r1", "b.ts");
    renderTable();
    expect(screen.getByText("2 of 4 files viewed")).toBeInTheDocument();
  });

  it("shows Building and the stage while creating, with no time left", () => {
    renderTable({ reviews: [review("r2", "CREATING", { stage: "applying", totals: null })] });
    const row = screen.getByRole("row", { name: /atlas\/server/ });
    expect(row).toHaveTextContent("Building");
    expect(within(row).getByRole("button", { name: "Open" })).toBeDisabled();
  });

  it("offers Inspect for a conflicted review", () => {
    renderTable({
      reviews: [
        review("r3", "CONFLICTED", {
          error: { code: "MERGE_CONFLICT", message: "conflict" },
        }),
      ],
    });
    expect(screen.getByRole("button", { name: "Inspect" })).toBeInTheDocument();
    expect(screen.getByText("MERGE_CONFLICT")).toBeInTheDocument();
  });

  it("opens the review when the row is clicked away from the actions", async () => {
    const { onOpen } = renderTable();
    await userEvent.click(screen.getByText("atlas/server"));
    expect(onOpen).toHaveBeenCalledWith("r1");
  });

  it("does not open the review when an action button is clicked", async () => {
    const { onOpen } = renderTable();
    await userEvent.click(screen.getByRole("button", { name: "Resume" }));
    expect(onOpen).toHaveBeenCalledTimes(1);
    onOpen.mockClear();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("confirms before discarding", async () => {
    const { onDiscard } = renderTable();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    expect(onDiscard).toHaveBeenCalledWith("r1");
  });

  it("always renders the start-a-new-review row", async () => {
    const { onNewReview } = renderTable({ reviews: [] });
    expect(screen.getByText("No open reviews")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /start a new review/i }));
    expect(onNewReview).toHaveBeenCalled();
  });

  it("renders skeleton rows while loading", () => {
    renderTable({ reviews: [], loading: true });
    expect(screen.queryByText("No open reviews")).not.toBeInTheDocument();
  });
});
