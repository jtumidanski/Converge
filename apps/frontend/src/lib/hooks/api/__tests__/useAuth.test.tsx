import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import {
  authKeys,
  useAuthMode,
  useCurrentUser,
  useLogin,
  useLogout,
} from "@/lib/hooks/api/useAuth";
import { userProviderKeys } from "@/lib/hooks/api/useUserProviders";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("useAuthMode", () => {
  it("returns the parsed mode and does not refetch on a second mount", async () => {
    let requestCount = 0;
    server.use(
      http.get("/api/auth/mode", () => {
        requestCount += 1;
        return HttpResponse.json(
          oneDoc("modes", "current", { mode: "hosted", registrationOpen: true }),
        );
      }),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    const first = renderHook(() => useAuthMode(), { wrapper });
    await waitFor(() => expect(first.result.current.data?.mode).toBe("hosted"));
    expect(requestCount).toBe(1);

    const second = renderHook(() => useAuthMode(), { wrapper });
    await waitFor(() => expect(second.result.current.data?.mode).toBe("hosted"));
    expect(requestCount).toBe(1);
  });
});

describe("useCurrentUser", () => {
  it("does not retry on a 401 — one request only", async () => {
    let requestCount = 0;
    server.use(
      http.get("/api/auth/me", () => {
        requestCount += 1;
        return HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), {
          status: 401,
        });
      }),
    );
    const { result } = renderHook(() => useCurrentUser(), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(requestCount).toBe(1);
  });
});

describe("useLogin", () => {
  it("invalidates auth.me on success", async () => {
    server.use(
      http.post("/api/auth/login", () =>
        HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        ),
      ),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    wrapper.client.setQueryData(authKeys.me, { id: "old" });
    const { result } = renderHook(() => useLogin(), { wrapper });
    result.current.mutate({ username: "alice", password: "12345678" });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await waitFor(() =>
      expect(wrapper.client.getQueryState(authKeys.me)?.isInvalidated).toBe(true),
    );
  });
});

describe("useLogout", () => {
  it("clears the cache on success, so a previously cached providers entry is gone", async () => {
    server.use(http.post("/api/auth/logout", () => new HttpResponse(null, { status: 204 })));
    const wrapper = queryWrapper({ gcTime: Infinity });
    wrapper.client.setQueryData(userProviderKeys.all, [{ id: "p1" }]);
    const { result } = renderHook(() => useLogout(), { wrapper });
    result.current.mutate();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(wrapper.client.getQueryData(userProviderKeys.all)).toBeUndefined();
  });
});
