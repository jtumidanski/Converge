import { describe, expect, it } from "vitest";
import { viewedProgress } from "@/lib/review/progress";
import type { ReviewFile } from "@/types/models/reviewFile";

function file(path: string): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 0,
      deletions: 0,
      binary: false,
    },
  };
}

describe("viewedProgress", () => {
  it("counts viewed files over the total", () => {
    const files = [file("a.ts"), file("b.ts"), file("c.ts")];
    expect(viewedProgress(files, new Set(["a.ts", "c.ts"]))).toEqual({
      viewed: 2,
      total: 3,
      percent: 67,
    });
  });

  it("ignores viewed paths that are no longer in the file list", () => {
    expect(viewedProgress([file("a.ts")], new Set(["a.ts", "gone.ts"]))).toEqual({
      viewed: 1,
      total: 1,
      percent: 100,
    });
  });

  it("reports zero percent for an empty file list rather than NaN", () => {
    expect(viewedProgress([], new Set())).toEqual({ viewed: 0, total: 0, percent: 0 });
  });
});
