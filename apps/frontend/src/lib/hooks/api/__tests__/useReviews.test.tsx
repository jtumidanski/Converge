import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { useCreateReview, useReview, useReviewFiles } from "@/lib/hooks/api/useReviews";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  vi.useRealTimers();
});
afterAll(() => server.close());

function reviewAttrs(status: string) {
  return {
    status,
    stage: status === "CREATING" ? "resolving" : null,
    provider: "gh",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: status === "READY" ? "a".repeat(40) : null,
    headSha: status === "READY" ? "b".repeat(40) : null,
    baseDescription: "Immediately before #421",
    changes: [421],
    included: [],
    totals: status === "READY" ? { files: 1, additions: 2, deletions: 0 } : null,
    error: null,
    createdAt: "2026-09-01T12:00:00Z",
    updatedAt: "2026-09-01T12:00:05Z",
    expiresAt: "2026-09-02T12:00:00Z",
  };
}

describe("useReview", () => {
  it("polls while CREATING and stops once terminal", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews/abc12345", () => {
        calls += 1;
        return HttpResponse.json(
          oneDoc("reviews", "abc12345", reviewAttrs(calls < 2 ? "CREATING" : "READY")),
        );
      }),
    );
    const { result } = renderHook(() => useReview("abc12345"), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.data?.attributes.status).toBe("CREATING"));
    await waitFor(() => expect(result.current.data?.attributes.status).toBe("READY"), {
      timeout: 6000,
    });
    const callsAtReady = calls;
    await new Promise((resolve) => setTimeout(resolve, 2500));
    expect(calls).toBe(callsAtReady);
  }, 15000);

  it("does not fetch files until the review is READY", async () => {
    let fileCalls = 0;
    server.use(
      http.get("/api/reviews/abc12345/files", () => {
        fileCalls += 1;
        return HttpResponse.json({ data: [] });
      }),
    );
    const disabled = renderHook(() => useReviewFiles("abc12345", false), {
      wrapper: queryWrapper(),
    });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(fileCalls).toBe(0);
    expect(disabled.result.current.data).toBeUndefined();
    renderHook(() => useReviewFiles("abc12345", true), { wrapper: queryWrapper() });
    await waitFor(() => expect(fileCalls).toBe(1));
  });
});

describe("useCreateReview", () => {
  it("posts the request and returns the created review", async () => {
    server.use(
      http.post("/api/reviews", () =>
        HttpResponse.json(oneDoc("reviews", "abc12345", reviewAttrs("CREATING")), { status: 202 }),
      ),
    );
    const { result } = renderHook(() => useCreateReview(), { wrapper: queryWrapper() });
    const created = await result.current.mutateAsync({
      provider: "gh",
      repository: "atlas/server",
      changes: [421],
    });
    expect(created.id).toBe("abc12345");
  });
});
