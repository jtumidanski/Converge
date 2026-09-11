import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { ReviewPage } from "@/pages/ReviewPage";
import { HttpResponse, http, listDoc, oneDoc, server } from "@/test/server";
import { viewedKey } from "@/lib/storage/viewed";
import type { ReviewAttributes } from "@/types/models/review";

vi.mock("@/components/features/review/FileDiff", () => ({
  FileDiff: () => <div data-testid="file-diff" />,
}));

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

let deleted: string[] = [];

function reviewAttrs(
  status: ReviewAttributes["status"],
  overrides: Partial<ReviewAttributes> = {},
): ReviewAttributes {
  const hasWorkspace = status === "READY" || status === "CONFLICTED" || status === "FAILED";
  return {
    status,
    stage: null,
    provider: "gl",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: hasWorkspace ? "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b" : null,
    headSha: null,
    baseDescription: "before the squash landed",
    changes: [421],
    included: [
      {
        number: 421,
        title: "ATLAS-7 add a thing",
        author: "jsmith",
        mergedAt: "2026-01-01T00:00:00Z",
        webUrl: "https://gitlab.test/atlas/server/-/merge_requests/421",
        strategy: "squash",
      },
    ],
    totals: hasWorkspace ? { files: 2, additions: 10, deletions: 2 } : null,
    error: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    expiresAt: "2999-01-01T00:00:00Z",
    ...overrides,
  };
}

function reviewFile(path: string) {
  return {
    type: "review-files" as const,
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified" as const,
      additions: 5,
      deletions: 1,
      binary: false,
    },
  };
}

function seed() {
  deleted = [];
  server.use(
    http.get("/api/reviews/:id", () =>
      HttpResponse.json(oneDoc("reviews", "rev-1", reviewAttrs("READY"))),
    ),
    http.get("/api/reviews/:id/files", () =>
      HttpResponse.json(listDoc([reviewFile("src/a.ts"), reviewFile("src/b.ts")])),
    ),
    http.get("/api/reviews/:id/files/*", ({ request }) => {
      const path = new URL(request.url).pathname.split("/files/")[1] ?? "";
      return HttpResponse.json(
        oneDoc("review-file-diffs", path, {
          path: decodeURIComponent(path),
          previousPath: "",
          status: "modified",
          additions: 5,
          deletions: 1,
          binary: false,
          truncated: false,
          diff: "diff --git a/x b/x\n",
        }),
      );
    }),
    http.delete("/api/reviews/:id", ({ params }) => {
      deleted.push(String(params.id));
      return new HttpResponse(null, { status: 204 });
    }),
    http.get("/api/providers/gl/repositories/:repo", () =>
      HttpResponse.json(
        oneDoc("repositories", "atlas/server", {
          name: "server",
          namespace: "atlas",
          defaultBranch: "main",
          webUrl: "https://gitlab.test/atlas/server",
        }),
      ),
    ),
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          {
            type: "providers" as const,
            id: "gl",
            attributes: { kind: "gitlab", displayName: "GitLab", baseUrl: "https://gitlab.test" },
          },
        ]),
      ),
    ),
  );
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/reviews/rev-1"]}>
          <AppShell>
            <Routes>
              <Route path="/reviews/:id" element={<ReviewPage />} />
              <Route path="/" element={<p>reviews page</p>} />
            </Routes>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => localStorage.clear());

describe("ReviewPage (READY)", () => {
  it("publishes a breadcrumb with the repository and change numbers", async () => {
    seed();
    renderPage();
    await waitFor(() => {
      const nav = screen.getByRole("navigation", { name: /breadcrumb/i });
      expect(nav).toHaveTextContent("atlas/server");
      expect(nav).toHaveTextContent("#421");
    });
  });

  it("selects the first file and shows its diff", async () => {
    seed();
    renderPage();
    expect(await screen.findByTestId("file-diff")).toBeInTheDocument();
    expect(screen.getByText("File 1 of 2")).toBeInTheDocument();
  });

  it("selects a file the user clicks instead of always the first one", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    expect(screen.getByText("File 1 of 2")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("treeitem", { name: /src\/b\.ts/ }));
    await waitFor(() => expect(screen.getByText("File 2 of 2")).toBeInTheDocument());
  });

  it("moves between files with j and k", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("j");
    expect(await screen.findByText("File 2 of 2")).toBeInTheDocument();
    await userEvent.keyboard("k");
    expect(await screen.findByText("File 1 of 2")).toBeInTheDocument();
  });

  it("wraps to the first file from the last", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("j");
    await screen.findByRole("button", { name: /back to first file/i });
    await userEvent.keyboard("j");
    expect(await screen.findByText("File 1 of 2")).toBeInTheDocument();
  });

  it("toggles viewed with v and persists it", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("v");
    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem(viewedKey("rev-1")) ?? "[]")).toEqual(["src/a.ts"]),
    );
    expect(screen.getByText("1 / 2 viewed")).toBeInTheDocument();
  });

  it("links the file header to the single included change", async () => {
    seed();
    renderPage();
    expect(await screen.findByRole("link", { name: /open in gitlab/i })).toHaveAttribute(
      "href",
      "https://gitlab.test/atlas/server/-/merge_requests/421",
    );
  });

  it("finishes the review, clears viewed state, and returns to the root", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("v");
    await userEvent.click(screen.getByRole("button", { name: /finish review/i }));
    await waitFor(() => expect(deleted).toEqual(["rev-1"]));
    expect(localStorage.getItem(viewedKey("rev-1"))).toBeNull();
    expect(await screen.findByText("reviews page")).toBeInTheDocument();
  });

  it("does not fire shortcuts while the tree filter has focus", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.click(screen.getByPlaceholderText(/filter files/i));
    await userEvent.keyboard("j");
    expect(screen.getByText("File 1 of 2")).toBeInTheDocument();
  });

  it("shows an error banner, not an empty file tree, when the file list fails to load", async () => {
    seed();
    server.use(
      http.get("/api/reviews/:id/files", () => HttpResponse.json({ errors: [] }, { status: 500 })),
    );
    renderPage();
    expect(await screen.findByText(/could not load the file list/i)).toBeInTheDocument();
    expect(screen.queryByText(/no file changes/i)).not.toBeInTheDocument();
  });

  it("shows an error banner, not a stale diff, when a single file's diff fails to load", async () => {
    seed();
    server.use(
      http.get("/api/reviews/:id/files/*", () =>
        HttpResponse.json({ errors: [] }, { status: 500 }),
      ),
    );
    renderPage();
    expect(await screen.findByText(/could not load this file's diff/i)).toBeInTheDocument();
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument();
  });
});

describe("ReviewPage (other statuses)", () => {
  it("shows the building stage while the review is still creating", async () => {
    server.use(
      http.get("/api/reviews/:id", () =>
        HttpResponse.json(
          oneDoc("reviews", "rev-1", reviewAttrs("CREATING", { stage: "applying:421" })),
        ),
      ),
    );
    renderPage();
    expect(await screen.findByText(/applying #421/i)).toBeInTheDocument();
  });

  it("shows an error banner, not the diff layout, when the review itself fails to load", async () => {
    server.use(
      http.get("/api/reviews/:id", () => HttpResponse.json({ errors: [] }, { status: 500 })),
    );
    renderPage();
    expect(await screen.findByText(/could not load this review/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
  });

  it("renders the error panel for a failed review", async () => {
    server.use(
      http.get("/api/reviews/:id", () =>
        HttpResponse.json(
          oneDoc(
            "reviews",
            "rev-1",
            reviewAttrs("FAILED", {
              error: {
                code: "NOT_MERGED",
                message: "#421 is not merged yet. Converge can only reconstruct merged PRs/MRs.",
                change: 421,
              },
            }),
          ),
        ),
      ),
      http.delete("/api/reviews/:id", () => new HttpResponse(null, { status: 204 })),
    );
    renderPage();
    expect(await screen.findByText(/is not merged yet/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    await waitFor(() => expect(screen.getByText("reviews page")).toBeInTheDocument());
  });

  it("renders the error panel, not the diff layout, for a conflicted review", async () => {
    server.use(
      http.get("/api/reviews/:id", () =>
        HttpResponse.json(
          oneDoc(
            "reviews",
            "rev-1",
            reviewAttrs("CONFLICTED", {
              error: {
                code: "CONFLICT",
                message: "#435 conflicts while being applied.",
                change: 435,
                conflictingFiles: ["src/a.ts"],
              },
            }),
          ),
        ),
      ),
      http.delete("/api/reviews/:id", () => new HttpResponse(null, { status: 204 })),
    );
    renderPage();
    expect(await screen.findByText(/#435 conflicts/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /discard review/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
  });

  it("renders a terminal panel, not the diff layout or Finish Review, for a finished review", async () => {
    server.use(
      http.get("/api/reviews/:id", () =>
        HttpResponse.json(oneDoc("reviews", "rev-1", reviewAttrs("FINISHED"))),
      ),
    );
    renderPage();
    expect(await screen.findByText(/no longer available/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /start a new review/i }));
    expect(await screen.findByText("reviews page")).toBeInTheDocument();
  });

  it("renders a terminal panel, not the diff layout or Finish Review, for an expired review", async () => {
    server.use(
      http.get("/api/reviews/:id", () =>
        HttpResponse.json(oneDoc("reviews", "rev-1", reviewAttrs("EXPIRED"))),
      ),
    );
    renderPage();
    expect(await screen.findByText(/no longer available/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
  });
});
