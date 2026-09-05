import { describe, expect, it } from "vitest";
import { repositorySchema } from "@/lib/schemas/repository";

/**
 * Parity fixtures lifted from apps/backend/internal/gitx/validate_test.go
 * (TestValidateRepoFullName). The schema mirrors ValidateRepoFullName
 * exactly: every identifier the backend accepts must pass here, and every
 * one it rejects must fail here.
 */
const BACKEND_ACCEPTS = ["owner/repo", "group/sub/project", "a.b/c-d_e"];

const BACKEND_REJECTS = [
  "",
  "repo",
  "/owner/repo",
  "-owner/repo",
  ".owner/repo",
  "owner/../repo",
  "owner/./repo",
  "owner//repo",
  "owner/repo/",
  "owner/repo name",
  "owner/repo\x00",
  "owner/re\npo",
  "../x/y",
];

describe("repositorySchema", () => {
  it.each(BACKEND_ACCEPTS)("accepts %s, matching the backend validator", (value) => {
    const result = repositorySchema.safeParse({ repository: value });
    expect(result.success).toBe(true);
  });

  it.each(BACKEND_REJECTS)("rejects %s, matching the backend validator", (value) => {
    const result = repositorySchema.safeParse({ repository: value });
    expect(result.success).toBe(false);
  });

  it("rejects an interior '.' segment (the gap fixed on this round)", () => {
    const result = repositorySchema.safeParse({ repository: "owner/./name" });
    expect(result.success).toBe(false);
  });

  it("rejects an interior '..' segment", () => {
    const result = repositorySchema.safeParse({ repository: "owner/../name" });
    expect(result.success).toBe(false);
  });
});
