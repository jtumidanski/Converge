import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";
import { strings } from "@/lib/strings";

describe("cn", () => {
  it("merges conditional classes and resolves conflicts", () => {
    expect(cn("p-2", "p-4")).toBe("p-4");
    const isHidden: boolean = false;
    expect(cn("flex", isHidden && "hidden", "items-center")).toBe("flex items-center");
  });
});

describe("strings", () => {
  it("uses only product vocabulary outside diagnostics", () => {
    const productCopy = Object.entries(strings)
      .filter(([key]) => key !== "diagnostics")
      .map(([, value]) => value)
      .join(" ")
      .toLowerCase();
    for (const gitTerm of ["worktree", "cherry-pick", "synthetic branch"]) {
      expect(productCopy).not.toContain(gitTerm);
    }
    expect(strings.finishReview).toBe("Finish Review");
    expect(strings.discardReview).toBe("Discard Review");
    expect(strings.combinedReview).toBe("Combined Review");
  });
});
