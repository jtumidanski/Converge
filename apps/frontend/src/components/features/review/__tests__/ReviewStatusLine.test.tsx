import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ReviewStatusLine } from "@/components/features/review/ReviewStatusLine";
import type { IncludedChange, Review } from "@/types/models/review";
import type { ReviewFile } from "@/types/models/reviewFile";

function included(number: number, title: string): IncludedChange {
  return {
    number,
    title,
    author: "jsmith",
    mergedAt: "2026-01-01T00:00:00Z",
    webUrl: `https://example.test/mr/${number}`,
    strategy: "squash",
  };
}

function review(overrides: Partial<Review["attributes"]> = {}): Review {
  return {
    type: "reviews",
    id: "r1",
    attributes: {
      status: "READY",
      stage: null,
      provider: "gl",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b",
      headSha: null,
      baseDescription: "before the squash landed",
      changes: [421, 430],
      included: [included(421, "ATLAS-7 add a thing"), included(430, "tidy up")],
      totals: { files: 4, additions: 120, deletions: 30 },
      error: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      expiresAt: "2999-01-01T00:00:00Z",
      ...overrides,
    },
  };
}

function file(path: string): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 1,
      deletions: 1,
      binary: false,
    },
  };
}

function renderLine(props: Partial<React.ComponentProps<typeof ReviewStatusLine>> = {}) {
  const onFinish = vi.fn();
  const onDiscard = vi.fn();
  render(
    <ReviewStatusLine
      review={review()}
      files={[file("a.ts"), file("b.ts"), file("c.ts"), file("d.ts")]}
      viewed={new Set(["a.ts"])}
      onFinish={onFinish}
      onDiscard={onDiscard}
      pending={false}
      {...props}
    />,
  );
  return { onFinish, onDiscard };
}

describe("ReviewStatusLine", () => {
  it("shows the ticket badge and one badge per included change", () => {
    renderLine();
    expect(screen.getByText("ATLAS-7")).toBeInTheDocument();
    expect(screen.getByText("#421")).toBeInTheDocument();
    expect(screen.getByText("#430")).toBeInTheDocument();
  });

  it("shows the base branch, short sha, and description", () => {
    renderLine();
    expect(screen.getByText(/main @ 6140736/)).toBeInTheDocument();
    expect(screen.getByText(/before the squash landed/)).toBeInTheDocument();
  });

  it("shows totals and viewed progress", () => {
    renderLine();
    expect(screen.getByText(/4 files/)).toBeInTheDocument();
    expect(screen.getByText("+120")).toBeInTheDocument();
    expect(screen.getByText("−30")).toBeInTheDocument();
    expect(screen.getByText("1 / 4 viewed")).toBeInTheDocument();
  });

  it("lists the included changes in a popover", async () => {
    renderLine();
    await userEvent.click(screen.getByRole("button", { name: /details/i }));
    expect(await screen.findByText("ATLAS-7 add a thing")).toBeInTheDocument();
    expect(screen.getByText("tidy up")).toBeInTheDocument();
    expect(screen.getAllByText(/squash/)).not.toHaveLength(0);
  });

  it("finishes without a confirmation", async () => {
    const { onFinish } = renderLine();
    await userEvent.click(screen.getByRole("button", { name: /finish review/i }));
    expect(onFinish).toHaveBeenCalled();
  });

  it("confirms before discarding", async () => {
    const { onDiscard } = renderLine();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    expect(onDiscard).toHaveBeenCalled();
  });

  it("omits the ticket badge when no included title carries a key", () => {
    renderLine({ review: review({ included: [included(1, "tidy up")] }) });
    expect(screen.queryByText(/[A-Z]+-\d+/)).not.toBeInTheDocument();
  });
});
