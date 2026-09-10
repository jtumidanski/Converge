import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { changesService, repositoriesService, reviewsService } from "@/services/api";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("services", () => {
  it("lists repositories with page meta", async () => {
    server.use(
      http.get("/api/providers/gh/repositories", ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get("page")).toBe("2");
        expect(url.searchParams.get("pageSize")).toBe("30");
        return HttpResponse.json(
          listDoc(
            [
              oneDoc("repositories", "atlas/server", {
                name: "server",
                namespace: "atlas",
                defaultBranch: "main",
                webUrl: "u",
              }).data,
            ],
            { number: 2, size: 30, hasNext: false },
          ),
        );
      }),
    );
    const out = await repositoriesService.list("gh", { page: 2, pageSize: 30 });
    expect(out.items).toHaveLength(1);
    expect(out.items[0]?.attributes.defaultBranch).toBe("main");
    expect(out.page?.hasNext).toBe(false);
  });

  it("encodes the repository path segment when listing changes", async () => {
    let seenPath = "";
    server.use(
      http.get("/api/providers/gh/repositories/:repo/changes", ({ params, request }) => {
        seenPath = String(params.repo);
        expect(new URL(request.url).searchParams.get("target")).toBe("main");
        return HttpResponse.json(listDoc([], { number: 1, size: 30, hasNext: false }));
      }),
    );
    await changesService.list("gh", "atlas/server", { target: "main" });
    expect(seenPath).toBe("atlas/server");
  });

  it("creates, reads and deletes a review", async () => {
    server.use(
      http.post("/api/reviews", async ({ request }) => {
        const body = (await request.json()) as {
          data: { type: string; attributes: Record<string, unknown> };
        };
        expect(body.data.type).toBe("reviews");
        expect(body.data.attributes.changes).toEqual([421, 427]);
        return HttpResponse.json(oneDoc("reviews", "7f14b2c8", { status: "CREATING" }), {
          status: 202,
        });
      }),
      http.get("/api/reviews/7f14b2c8", () =>
        HttpResponse.json(oneDoc("reviews", "7f14b2c8", { status: "READY" })),
      ),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json(
          listDoc([
            oneDoc("review-files", "a.txt", {
              path: "a.txt",
              status: "added",
              additions: 1,
              deletions: 0,
              binary: false,
              previousPath: "",
            }).data,
          ]),
        ),
      ),
      http.get("/api/reviews/7f14b2c8/files/a.txt", () =>
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
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    const created = await reviewsService.create({
      provider: "gh",
      repository: "atlas/server",
      changes: [421, 427],
    });
    expect(created.id).toBe("7f14b2c8");
    expect((await reviewsService.get("7f14b2c8")).attributes.status).toBe("READY");
    expect(await reviewsService.files("7f14b2c8")).toHaveLength(1);
    expect((await reviewsService.fileDiff("7f14b2c8", "a.txt")).attributes.diff).toBe("+a");
    await expect(reviewsService.remove("7f14b2c8")).resolves.toBeUndefined();
  });

  it("throws instead of returning an empty list when the server response is missing data", async () => {
    server.use(http.get("/api/reviews", () => HttpResponse.json({ notData: [] })));
    await expect(reviewsService.list()).rejects.toThrow(/Malformed JSON:API response/);
  });

  it("throws instead of returning undefined when a single-resource response is missing data", async () => {
    server.use(http.get("/api/reviews/missing", () => HttpResponse.json({})));
    await expect(reviewsService.get("missing")).rejects.toThrow(/Malformed JSON:API response/);
  });
});
