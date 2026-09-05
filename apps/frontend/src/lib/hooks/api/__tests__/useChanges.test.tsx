import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/errors";
import { errorDoc, http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { changeKeys, useChanges } from "@/lib/hooks/api/useChanges";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function changeAttrs(n: number) {
  return {
    number: n,
    title: `change ${n}`,
    author: "dev",
    sourceBranch: "feat",
    targetBranch: "main",
    mergedAt: "2026-08-21T14:02:11Z",
    createdAt: "2026-08-20T09:00:00Z",
    landingSha: null,
    webUrl: "https://example.test",
  };
}

describe("useChanges", () => {
  it("does not fetch when providerId or repository is undefined", async () => {
    let calls = 0;
    server.use(
      http.get("/api/providers/:id/repositories/:repo/changes", () => {
        calls += 1;
        return HttpResponse.json(listDoc([]));
      }),
    );
    renderHook(() => useChanges(undefined, "atlas/server", {}), { wrapper: queryWrapper() });
    renderHook(() => useChanges("gh", undefined, {}), { wrapper: queryWrapper() });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(calls).toBe(0);
  });

  it("fetches changes for provider and repository, keyed by changeKeys.list", async () => {
    server.use(
      http.get("/api/providers/gh/repositories/:repo/changes", ({ request, params }) => {
        expect(params.repo).toBe("atlas/server");
        expect(new URL(request.url).searchParams.get("target")).toBe("main");
        return HttpResponse.json(listDoc([oneDoc("changes", "421", changeAttrs(421)).data]));
      }),
    );
    const wrapper = queryWrapper();
    const params = { target: "main" };
    const { result } = renderHook(() => useChanges("gh", "atlas/server", params), { wrapper });
    await waitFor(() => expect(result.current.data?.items).toHaveLength(1));
    expect(wrapper.client.getQueryData(changeKeys.list("gh", "atlas/server", params))).toEqual(
      result.current.data,
    );
  });

  it("surfaces the ApiError rather than an empty list on failure", async () => {
    server.use(
      http.get("/api/providers/gh/repositories/:repo/changes", () =>
        HttpResponse.json(errorDoc(502, "REPOSITORY_UNAVAILABLE", "down"), { status: 502 }),
      ),
    );
    const { result } = renderHook(() => useChanges("gh", "atlas/server", {}), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as ApiError).code).toBe("REPOSITORY_UNAVAILABLE");
    expect(result.current.data).toBeUndefined();
  });
});
