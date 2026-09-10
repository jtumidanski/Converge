import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse, server } from "@/test/server";
import { apiDelete, apiGet, apiGetText, apiPost } from "@/lib/api/client";
import { ApiError, isApiError, messageFor } from "@/lib/api/errors";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("api client", () => {
  it("sends JSON:API headers and returns the parsed body", async () => {
    let seenAccept = "";
    server.use(
      http.get("/api/thing", ({ request }) => {
        seenAccept = request.headers.get("Accept") ?? "";
        return HttpResponse.json({ data: { type: "thing", id: "1", attributes: { ok: true } } });
      }),
    );
    const body = await apiGet<{ data: { id: string } }>("/api/thing");
    expect(body.data.id).toBe("1");
    expect(seenAccept).toContain("application/vnd.api+json");
  });

  it("posts a JSON:API document and returns the response", async () => {
    let received: unknown = null;
    server.use(
      http.post("/api/reviews", async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ data: { type: "reviews", id: "abc" } }, { status: 202 });
      }),
    );
    const out = await apiPost<{ data: { id: string } }>("/api/reviews", {
      data: { type: "reviews", attributes: { provider: "gh" } },
    });
    expect(out.data.id).toBe("abc");
    expect(received).toEqual({ data: { type: "reviews", attributes: { provider: "gh" } } });
  });

  it("throws ApiError carrying status, code and detail", async () => {
    server.use(
      http.get("/api/bad", () =>
        HttpResponse.json(
          {
            errors: [
              { status: "400", code: "INVALID_CHANGES", title: "Bad Request", detail: "Pick one." },
            ],
          },
          { status: 400 },
        ),
      ),
    );
    const error = await apiGet("/api/bad").catch((e: unknown) => e);
    expect(isApiError(error)).toBe(true);
    const apiError = error as ApiError;
    expect(apiError.status).toBe(400);
    expect(apiError.code).toBe("INVALID_CHANGES");
    expect(apiError.detail).toBe("Pick one.");
    expect(messageFor(apiError, "fallback")).toBe("Pick one.");
    expect(messageFor(new Error("boom"), "fallback")).toBe("fallback");
  });

  it("handles error responses that are not JSON:API documents", async () => {
    server.use(http.get("/api/html", () => new HttpResponse("<html>502</html>", { status: 502 })));
    const error = (await apiGet("/api/html").catch((e: unknown) => e)) as ApiError;
    expect(error.status).toBe(502);
    expect(error.code).toBe("UNKNOWN");
  });

  it("returns nothing for 204 and text for text endpoints", async () => {
    server.use(http.delete("/api/reviews/abc", () => new HttpResponse(null, { status: 204 })));
    await expect(apiDelete("/api/reviews/abc")).resolves.toBeUndefined();
    server.use(http.get("/api/reviews/abc/diff", () => HttpResponse.text("diff --git a/x b/x")));
    await expect(apiGetText("/api/reviews/abc/diff")).resolves.toContain("diff --git");
  });
});
