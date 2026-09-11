import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { errorDoc, http, HttpResponse, server } from "@/test/server";
import {
  apiDelete,
  apiGet,
  apiGetText,
  apiPatch,
  apiPost,
  setUnauthorizedHandler,
} from "@/lib/api/client";
import { ApiError, isApiError, messageFor } from "@/lib/api/errors";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
beforeEach(() => setUnauthorizedHandler(null));

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

  it("sends credentials on every request", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    server.use(
      http.get("/api/creds-get", () => HttpResponse.json({ data: { type: "x", id: "1" } })),
      http.post("/api/creds-post", () => HttpResponse.json({ data: { type: "x", id: "1" } })),
    );
    await apiGet("/api/creds-get");
    await apiPost("/api/creds-post", { data: {} });
    expect(
      fetchSpy.mock.calls.every(([, init]) => (init as RequestInit).credentials === "include"),
    ).toBe(true);
    fetchSpy.mockRestore();
  });

  it("invokes the unauthorized handler on 401 and still throws", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    server.use(
      http.get("/api/needs-auth", () =>
        HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), { status: 401 }),
      ),
    );
    const error = (await apiGet("/api/needs-auth").catch((e: unknown) => e)) as ApiError;
    expect(handler).toHaveBeenCalledTimes(1);
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(401);
  });

  it("does not invoke the unauthorized handler on other failures", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    server.use(
      http.get("/api/forbidden", () =>
        HttpResponse.json(errorDoc(403, "FORBIDDEN", "No"), { status: 403 }),
      ),
      http.get("/api/broken", () =>
        HttpResponse.json(errorDoc(500, "UNKNOWN", "Boom"), { status: 500 }),
      ),
    );
    await apiGet("/api/forbidden").catch(() => undefined);
    await apiGet("/api/broken").catch(() => undefined);
    expect(handler).not.toHaveBeenCalled();
  });

  it("setUnauthorizedHandler(null) detaches", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    setUnauthorizedHandler(null);
    server.use(
      http.get("/api/needs-auth-2", () =>
        HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), { status: 401 }),
      ),
    );
    await apiGet("/api/needs-auth-2").catch(() => undefined);
    expect(handler).not.toHaveBeenCalled();
  });

  it("invokes the unauthorized handler on a 401 carrying UNAUTHENTICATED", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    server.use(
      http.get("/api/session-dead", () =>
        HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), { status: 401 }),
      ),
    );
    const error = (await apiGet("/api/session-dead").catch((e: unknown) => e)) as ApiError;
    expect(handler).toHaveBeenCalledTimes(1);
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(401);
  });

  it("does not invoke the unauthorized handler on a 401 carrying INVALID_CREDENTIALS", async () => {
    // A signed-in user with a perfectly valid session can get a 401 here —
    // e.g. mistyping their current password on change-password or
    // delete-account. That must not look like a dead session.
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    server.use(
      http.post("/api/auth/password", () =>
        HttpResponse.json(
          errorDoc(401, "INVALID_CREDENTIALS", "The current password is incorrect."),
          { status: 401 },
        ),
      ),
    );
    const error = (await apiPost("/api/auth/password", {}).catch((e: unknown) => e)) as ApiError;
    expect(handler).not.toHaveBeenCalled();
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(401);
    expect(error.code).toBe("INVALID_CREDENTIALS");
  });

  it("invokes the unauthorized handler on a 401 with no recognisable error code", async () => {
    // A dead session must never go unnoticed, so an absent/unrecognised
    // code on a 401 is treated the same as UNAUTHENTICATED.
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    server.use(
      http.get("/api/opaque-401", () => new HttpResponse("<html>401</html>", { status: 401 })),
    );
    const error = (await apiGet("/api/opaque-401").catch((e: unknown) => e)) as ApiError;
    expect(handler).toHaveBeenCalledTimes(1);
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(401);
    expect(error.code).toBe("UNKNOWN");
  });

  it("apiPatch sends the JSON:API content type", async () => {
    let seenMethod = "";
    let seenContentType = "";
    server.use(
      http.patch("/api/patch-target", ({ request }) => {
        seenMethod = request.method;
        seenContentType = request.headers.get("Content-Type") ?? "";
        return HttpResponse.json({ data: { type: "x", id: "1" } });
      }),
    );
    await apiPatch("/api/patch-target", { data: {} });
    expect(seenMethod).toBe("PATCH");
    expect(seenContentType).toBe("application/vnd.api+json");
  });
});
