import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/errors";
import { errorDoc, http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { providerKeys, useProviders } from "@/lib/hooks/api/useProviders";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("useProviders", () => {
  it("fetches providers through providersService and caches them under providerKeys.lists()", async () => {
    server.use(
      http.get("/api/providers", () =>
        HttpResponse.json(
          listDoc([
            oneDoc("providers", "gh", {
              displayName: "GitHub",
              kind: "github",
              baseUrl: "https://github.com",
            }).data,
          ]),
        ),
      ),
    );
    const wrapper = queryWrapper();
    const { result } = renderHook(() => useProviders(), { wrapper });
    await waitFor(() => expect(result.current.data).toHaveLength(1));
    expect(result.current.data?.[0]?.attributes.displayName).toBe("GitHub");
    expect(wrapper.client.getQueryData(providerKeys.lists())).toEqual(result.current.data);
  });

  it("surfaces the ApiError instead of returning empty data on failure", async () => {
    server.use(
      http.get("/api/providers", () =>
        HttpResponse.json(errorDoc(503, "PROVIDER_UNAVAILABLE", "down"), { status: 503 }),
      ),
    );
    const { result } = renderHook(() => useProviders(), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as ApiError).code).toBe("PROVIDER_UNAVAILABLE");
    expect(result.current.data).toBeUndefined();
  });
});
