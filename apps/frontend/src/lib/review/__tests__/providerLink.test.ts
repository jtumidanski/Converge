import { describe, expect, it } from "vitest";
import { openInProviderHref } from "@/lib/review/providerLink";
import type { IncludedChange, Review } from "@/types/models/review";

function included(number: number): IncludedChange {
  return {
    number,
    title: `#${number}`,
    author: "jsmith",
    mergedAt: "2026-01-01T00:00:00Z",
    webUrl: `https://example.test/mr/${number}`,
    strategy: "squash",
  };
}

function review(changes: IncludedChange[]): Review {
  return {
    type: "reviews",
    id: "r1",
    attributes: {
      status: "READY",
      stage: null,
      provider: "gl",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: null,
      headSha: null,
      baseDescription: "",
      changes: changes.map((c) => c.number),
      included: changes,
      totals: null,
      error: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      expiresAt: "2026-01-02T00:00:00Z",
    },
  };
}

describe("openInProviderHref", () => {
  it("links to the single included change", () => {
    expect(openInProviderHref(review([included(421)]), "https://example.test/atlas/server")).toBe(
      "https://example.test/mr/421",
    );
  });

  it("falls back to the repository when several changes are included", () => {
    // The files endpoint does not attribute files to changes, and per-line
    // attribution is a PRD non-goal, so there is no honest per-file target.
    expect(
      openInProviderHref(review([included(1), included(2)]), "https://example.test/atlas/server"),
    ).toBe("https://example.test/atlas/server");
  });

  it("returns undefined when neither target is known", () => {
    expect(openInProviderHref(review([]), undefined)).toBeUndefined();
  });
});
