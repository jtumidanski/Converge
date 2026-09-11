import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/errors";
import { errorDoc, http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { repositoryKeys, useRepositories, useRepository } from "@/lib/hooks/api/useRepositories";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function repoAttrs() {
  return {
    name: "server",
    namespace: "atlas",
    defaultBranch: "main",
    webUrl: "https://example.test",
  };
}

describe("useRepositories", () => {
  it("does not fetch when providerId is undefined", async () => {
    let calls = 0;
    server.use(
      http.get("/api/providers/:id/repositories", () => {
        calls += 1;
        return HttpResponse.json(listDoc([]));
      }),
    );
    renderHook(() => useRepositories(undefined), { wrapper: queryWrapper() });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(calls).toBe(0);
  });

  it("fetches repositories for the given provider and params, keyed by repositoryKeys.list", async () => {
    server.use(
      http.get("/api/providers/gh/repositories", ({ request }) => {
        expect(new URL(request.url).searchParams.get("page")).toBe("2");
        return HttpResponse.json(
          listDoc([oneDoc("repositories", "atlas/server", repoAttrs()).data]),
        );
      }),
    );
    const wrapper = queryWrapper();
    const { result } = renderHook(() => useRepositories("gh", { page: 2 }), { wrapper });
    await waitFor(() => expect(result.current.data?.items).toHaveLength(1));
    expect(wrapper.client.getQueryData(repositoryKeys.list("gh", { page: 2 }))).toEqual(
      result.current.data,
    );
  });

  it("surfaces the ApiError rather than an empty list on failure", async () => {
    server.use(
      http.get("/api/providers/gh/repositories", () =>
        HttpResponse.json(errorDoc(502, "REPOSITORY_UNAVAILABLE", "down"), { status: 502 }),
      ),
    );
    const { result } = renderHook(() => useRepositories("gh"), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as ApiError).code).toBe("REPOSITORY_UNAVAILABLE");
    expect(result.current.data).toBeUndefined();
  });
});

describe("useRepositories search", () => {
  it("passes search through to the repositories endpoint", async () => {
    let seen = "";
    server.use(
      http.get("/api/providers/gl/repositories", ({ request }) => {
        seen = new URL(request.url).searchParams.toString();
        return HttpResponse.json(listDoc([]));
      }),
    );
    const { result } = renderHook(() => useRepositories("gl", { search: "serv", page: 1 }), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(seen).toContain("search=serv");
  });
});

describe("useRepository", () => {
  it("does not fetch when enabled is false", async () => {
    let calls = 0;
    server.use(
      http.get("/api/providers/gh/repositories/:fullName", () => {
        calls += 1;
        return HttpResponse.json(oneDoc("repositories", "atlas/server", repoAttrs()));
      }),
    );
    renderHook(() => useRepository("gh", "atlas/server", false), { wrapper: queryWrapper() });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(calls).toBe(0);
  });

  it("fetches the repository detail when enabled, keyed by repositoryKeys.detail", async () => {
    server.use(
      http.get("/api/providers/gh/repositories/:fullName", ({ params }) => {
        expect(params.fullName).toBe("atlas/server");
        return HttpResponse.json(oneDoc("repositories", "atlas/server", repoAttrs()));
      }),
    );
    const wrapper = queryWrapper();
    const { result } = renderHook(() => useRepository("gh", "atlas/server", true), { wrapper });
    await waitFor(() => expect(result.current.data?.attributes.name).toBe("server"));
    expect(wrapper.client.getQueryData(repositoryKeys.detail("gh", "atlas/server"))).toEqual(
      result.current.data,
    );
  });
});
