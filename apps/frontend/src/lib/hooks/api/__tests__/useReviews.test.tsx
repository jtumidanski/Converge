import { act, renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/errors";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import {
  reviewKeys,
  useCreateReview,
  useFinishReview,
  useInvalidateReviews,
  useReview,
  useReviewFile,
  useReviewFiles,
} from "@/lib/hooks/api/useReviews";

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

describe("useReviewFile", () => {
  it("fetches a single file's diff through reviewsService.fileDiff", async () => {
    server.use(
      http.get("/api/reviews/abc12345/files/a.txt", () =>
        HttpResponse.json(
          oneDoc("review-file-diffs", "a.txt", {
            path: "a.txt",
            status: "added",
            additions: 1,
            deletions: 0,
            binary: false,
            previousPath: "",
            truncated: false,
            diff: "+a",
          }),
        ),
      ),
    );
    const { result } = renderHook(() => useReviewFile("abc12345", "a.txt"), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.data?.attributes.diff).toBe("+a"));
  });

  it("does not fetch until both id and path are set", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews/abc12345/files/a.txt", () => {
        calls += 1;
        return HttpResponse.json({});
      }),
    );
    renderHook(() => useReviewFile("abc12345", undefined), { wrapper: queryWrapper() });
    renderHook(() => useReviewFile(undefined, "a.txt"), { wrapper: queryWrapper() });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(calls).toBe(0);
  });

  it("surfaces the ApiError instead of returning empty data on failure", async () => {
    server.use(
      http.get("/api/reviews/abc12345/files/a.txt", () =>
        HttpResponse.json(errorDoc(404, "NOT_FOUND", "not found"), { status: 404 }),
      ),
    );
    const { result } = renderHook(() => useReviewFile("abc12345", "a.txt"), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as ApiError).code).toBe("NOT_FOUND");
    expect(result.current.data).toBeUndefined();
  });
});

describe("useFinishReview", () => {
  it("calls reviewsService.remove and invalidates the review's detail and list caches", async () => {
    let deleteCalls = 0;
    server.use(
      http.delete("/api/reviews/abc12345", () => {
        deleteCalls += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    wrapper.client.setQueryData(reviewKeys.detail("abc12345"), { stale: "detail" });
    wrapper.client.setQueryData(reviewKeys.lists(), { stale: "list" });

    const { result } = renderHook(() => useFinishReview(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync("abc12345");
    });

    expect(deleteCalls).toBe(1);
    await waitFor(() =>
      expect(wrapper.client.getQueryState(reviewKeys.detail("abc12345"))?.isInvalidated).toBe(true),
    );
    expect(wrapper.client.getQueryState(reviewKeys.lists())?.isInvalidated).toBe(true);
  });

  it("surfaces the ApiError when the delete fails, without invalidating caches", async () => {
    server.use(
      http.delete("/api/reviews/abc12345", () =>
        HttpResponse.json(errorDoc(409, "REVIEW_NOT_READY", "not ready"), { status: 409 }),
      ),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    wrapper.client.setQueryData(reviewKeys.detail("abc12345"), { stale: "detail" });

    const { result } = renderHook(() => useFinishReview(), { wrapper });
    await act(async () => {
      await expect(result.current.mutateAsync("abc12345")).rejects.toBeInstanceOf(ApiError);
    });
    await waitFor(() => expect(result.current.error).toBeInstanceOf(ApiError));
    expect((result.current.error as ApiError).code).toBe("REVIEW_NOT_READY");
  });
});

describe("useInvalidateReviews", () => {
  it("invalidateReview targets only the given review's detail cache", async () => {
    const wrapper = queryWrapper({ gcTime: Infinity });
    wrapper.client.setQueryData(reviewKeys.detail("abc12345"), { marker: "abc" });
    wrapper.client.setQueryData(reviewKeys.detail("other"), { marker: "other" });
    wrapper.client.setQueryData(reviewKeys.lists(), { marker: "list" });

    const { result } = renderHook(() => useInvalidateReviews(), { wrapper });
    await act(async () => {
      await result.current.invalidateReview("abc12345");
    });

    expect(wrapper.client.getQueryState(reviewKeys.detail("abc12345"))?.isInvalidated).toBe(true);
    expect(wrapper.client.getQueryState(reviewKeys.detail("other"))?.isInvalidated).toBe(false);
    expect(wrapper.client.getQueryState(reviewKeys.lists())?.isInvalidated).toBe(false);
  });

  it("invalidateAll invalidates every reviews-scoped cache entry", async () => {
    const wrapper = queryWrapper({ gcTime: Infinity });
    wrapper.client.setQueryData(reviewKeys.detail("abc12345"), { marker: "abc" });
    wrapper.client.setQueryData(reviewKeys.lists(), { marker: "list" });

    const { result } = renderHook(() => useInvalidateReviews(), { wrapper });
    await act(async () => {
      await result.current.invalidateAll();
    });

    expect(wrapper.client.getQueryState(reviewKeys.detail("abc12345"))?.isInvalidated).toBe(true);
    expect(wrapper.client.getQueryState(reviewKeys.lists())?.isInvalidated).toBe(true);
  });
});
