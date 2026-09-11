import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { HttpResponse, http, listDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { useBranches } from "@/lib/hooks/api/useBranches";
import type { Branch } from "@/types/models/branch";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function branch(name: string, isDefault = false): Branch {
  return {
    type: "branches",
    id: `gl:atlas/server:${name}`,
    attributes: { name, isDefault, sha: "2222222222222222222222222222222222222222" },
  };
}

describe("useBranches", () => {
  it("requests the branches endpoint with the repository percent-encoded", async () => {
    let seen = "";
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", ({ request, params }) => {
        seen = `${String(params.repo)}?${new URL(request.url).searchParams.toString()}`;
        return HttpResponse.json(
          listDoc([branch("main", true)], { number: 1, size: 50, hasNext: false }),
        );
      }),
    );
    const { result } = renderHook(() => useBranches("gl", "atlas/server", { search: "ma" }), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.items.map((b) => b.attributes.name)).toEqual(["main"]);
    expect(result.current.data?.page?.hasNext).toBe(false);
    expect(seen).toBe("atlas/server?search=ma");
  });

  it("omits an empty search from the query string", async () => {
    let seen = "";
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", ({ request }) => {
        seen = new URL(request.url).search;
        return HttpResponse.json(listDoc([branch("main", true)]));
      }),
    );
    const { result } = renderHook(() => useBranches("gl", "atlas/server", { search: "" }), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(seen).toBe("");
  });

  it("stays disabled without a provider or repository", () => {
    const { result } = renderHook(() => useBranches(undefined, "atlas/server"), {
      wrapper: queryWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
  });

  it("can be disabled explicitly", () => {
    const { result } = renderHook(() => useBranches("gl", "atlas/server", {}, false), {
      wrapper: queryWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
  });
});
