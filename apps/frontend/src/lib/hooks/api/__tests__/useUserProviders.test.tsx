import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import {
  useCreateUserProvider,
  useDeleteUserProvider,
  useUpdateUserProvider,
  userProviderKeys,
} from "@/lib/hooks/api/useUserProviders";
import { providerKeys } from "@/lib/hooks/api/useProviders";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

const providerAttrs = {
  slug: "gh",
  displayName: "GitHub",
  kind: "github",
  baseUrl: "https://api.github.com",
  tokenLast4: "9f2c",
  tokenSetAt: "2026-01-01T00:00:00Z",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function seedInvalidatable(wrapper: ReturnType<typeof queryWrapper>): void {
  wrapper.client.setQueryData(userProviderKeys.all, []);
  wrapper.client.setQueryData(providerKeys.all, []);
}

async function expectBothInvalidated(wrapper: ReturnType<typeof queryWrapper>): Promise<void> {
  await waitFor(() =>
    expect(wrapper.client.getQueryState(userProviderKeys.all)?.isInvalidated).toBe(true),
  );
  await waitFor(() =>
    expect(wrapper.client.getQueryState(providerKeys.all)?.isInvalidated).toBe(true),
  );
}

describe("useCreateUserProvider", () => {
  it("invalidates userProviders and providers on success", async () => {
    server.use(
      http.post("/api/settings/providers", () =>
        HttpResponse.json(oneDoc("userProviders", "up1", providerAttrs), { status: 201 }),
      ),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    seedInvalidatable(wrapper);
    const { result } = renderHook(() => useCreateUserProvider(), { wrapper });
    result.current.mutate({
      slug: "gh",
      displayName: "GitHub",
      kind: "github",
      baseUrl: "",
      token: "ghp_xxx",
      validate: true,
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await expectBothInvalidated(wrapper);
  });
});

describe("useUpdateUserProvider", () => {
  it("invalidates userProviders and providers on success", async () => {
    server.use(
      http.patch("/api/settings/providers/up1", () =>
        HttpResponse.json(oneDoc("userProviders", "up1", providerAttrs)),
      ),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    seedInvalidatable(wrapper);
    const { result } = renderHook(() => useUpdateUserProvider(), { wrapper });
    result.current.mutate({ id: "up1", patch: { displayName: "GH renamed" } });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await expectBothInvalidated(wrapper);
  });
});

describe("useDeleteUserProvider", () => {
  it("invalidates userProviders and providers on success", async () => {
    server.use(
      http.delete("/api/settings/providers/up1", () => new HttpResponse(null, { status: 204 })),
    );
    const wrapper = queryWrapper({ gcTime: Infinity });
    seedInvalidatable(wrapper);
    const { result } = renderHook(() => useDeleteUserProvider(), { wrapper });
    result.current.mutate("up1");
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await expectBothInvalidated(wrapper);
  });
});
