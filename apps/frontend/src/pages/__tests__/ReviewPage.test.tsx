import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { ReviewPage } from "@/pages/ReviewPage";
import type { ReviewAttributes } from "@/types/models/review";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate, useParams: () => ({ id: "7f14b2c8" }) };
});

// The diff renderer pulls in Shiki; stub it for component tests.
vi.mock("@/components/features/review/FileDiff", () => ({
  FileDiff: ({ file }: { file: { attributes: { path: string; diff: string } } }) => (
    <pre data-testid="file-diff">{`${file.attributes.path}\n${file.attributes.diff}`}</pre>
  ),
}));

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
});
afterAll(() => server.close());

function reviewDoc(status: ReviewAttributes["status"], stage: string | null) {
  return oneDoc<"reviews", ReviewAttributes>("reviews", "7f14b2c8", {
    status,
    stage,
    provider: "gitlab-work",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: status === "READY" ? "9f21a43".padEnd(40, "0") : null,
    headSha: status === "READY" ? "b".repeat(40) : null,
    baseDescription: "Immediately before #421",
    changes: [421],
    included:
      status === "READY"
        ? [
            {
              number: 421,
              title: "Add field-state endpoint",
              author: "jsmith",
              mergedAt: "2026-08-21T14:02:11Z",
              webUrl: "https://example.test/421",
              strategy: "squash",
            },
          ]
        : [],
    totals: status === "READY" ? { files: 1, additions: 40, deletions: 12 } : null,
    error: null,
    createdAt: "2026-09-01T12:00:00Z",
    updatedAt: "2026-09-01T12:00:30Z",
    expiresAt: "2026-09-02T12:00:00Z",
  });
}

describe("ReviewPage", () => {
  it("shows the building stage then switches to the diff view", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews/7f14b2c8", () => {
        calls += 1;
        return HttpResponse.json(
          calls < 2 ? reviewDoc("CREATING", "applying:421") : reviewDoc("READY", null),
        );
      }),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json(
          listDoc([
            oneDoc("review-files", "src/field/FieldService.java", {
              path: "src/field/FieldService.java",
              previousPath: "",
              status: "modified",
              additions: 40,
              deletions: 12,
              binary: false,
            }).data,
          ]),
        ),
      ),
      http.get("/api/reviews/7f14b2c8/files/src/field/FieldService.java", () =>
        HttpResponse.json(
          oneDoc("review-file-diffs", "src/field/FieldService.java", {
            path: "src/field/FieldService.java",
            previousPath: "",
            status: "modified",
            additions: 40,
            deletions: 12,
            binary: false,
            truncated: false,
            diff: "@@ -1 +1 @@\n-old\n+new",
          }),
        ),
      ),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/applying #421/i)).toBeInTheDocument();
    expect(
      await screen.findByText(/Immediately before #421/, {}, { timeout: 6000 }),
    ).toBeInTheDocument();
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText(/main @ 9f21a43/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /#421/ })).toHaveAttribute(
      "href",
      "https://example.test/421",
    );
    // the first file is selected automatically
    expect(await screen.findByTestId("file-diff")).toHaveTextContent("+new");
  }, 15000);

  it("selects a file the user clicks instead of always the first one", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("READY", null))),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json(
          listDoc([
            oneDoc("review-files", "src/field/FieldService.java", {
              path: "src/field/FieldService.java",
              previousPath: "",
              status: "modified",
              additions: 40,
              deletions: 12,
              binary: false,
            }).data,
            oneDoc("review-files", "README.md", {
              path: "README.md",
              previousPath: "",
              status: "modified",
              additions: 2,
              deletions: 1,
              binary: false,
            }).data,
          ]),
        ),
      ),
      http.get("/api/reviews/7f14b2c8/files/src/field/FieldService.java", () =>
        HttpResponse.json(
          oneDoc("review-file-diffs", "src/field/FieldService.java", {
            path: "src/field/FieldService.java",
            previousPath: "",
            status: "modified",
            additions: 40,
            deletions: 12,
            binary: false,
            truncated: false,
            diff: "@@ -1 +1 @@\n-old\n+new",
          }),
        ),
      ),
      http.get("/api/reviews/7f14b2c8/files/README.md", () =>
        HttpResponse.json(
          oneDoc("review-file-diffs", "README.md", {
            path: "README.md",
            previousPath: "",
            status: "modified",
            additions: 2,
            deletions: 1,
            binary: false,
            truncated: false,
            diff: "@@ -1 +1 @@\n-old readme\n+new readme",
          }),
        ),
      ),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    // the first file is selected automatically
    expect(await screen.findByTestId("file-diff")).toHaveTextContent("src/field/FieldService.java");
    await userEvent.click(screen.getByRole("treeitem", { name: /README\.md/ }));
    await waitFor(() => expect(screen.getByTestId("file-diff")).toHaveTextContent("new readme"));
    expect(screen.getByTestId("file-diff")).not.toHaveTextContent("src/field/FieldService.java");
  });

  it("finishes the review and returns to the start", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("READY", null))),
      http.get("/api/reviews/7f14b2c8/files", () => HttpResponse.json(listDoc([]))),
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    await userEvent.click(await screen.findByRole("button", { name: /finish review/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/"));
  });

  it("renders the error panel for a failed review", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => {
        const doc = reviewDoc("FAILED", null);
        doc.data.attributes.error = {
          code: "NOT_MERGED",
          message: "#421 is not merged yet. Converge can only reconstruct merged PRs/MRs.",
          change: 421,
        };
        return HttpResponse.json(doc);
      }),
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/is not merged yet/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/"));
  });

  it("shows an error banner, not an empty file tree, when the file list fails to load", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("READY", null))),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json({ errors: [] }, { status: 500 }),
      ),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/could not load the file list/i)).toBeInTheDocument();
    // The empty-file-list affordance must not appear in place of the error.
    expect(screen.queryByText(/no file changes/i)).not.toBeInTheDocument();
  });

  it("shows an error banner, not the diff view, when the review itself fails to load", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json({ errors: [] }, { status: 500 })),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/could not load this review/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
  });

  it("shows an error banner, not a stale diff, when a single file's diff fails to load", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("READY", null))),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json(
          listDoc([
            oneDoc("review-files", "src/field/FieldService.java", {
              path: "src/field/FieldService.java",
              previousPath: "",
              status: "modified",
              additions: 40,
              deletions: 12,
              binary: false,
            }).data,
          ]),
        ),
      ),
      http.get("/api/reviews/7f14b2c8/files/src/field/FieldService.java", () =>
        HttpResponse.json({ errors: [] }, { status: 500 }),
      ),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/could not load this file's diff/i)).toBeInTheDocument();
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument();
  });

  it("renders a terminal panel, not the diff layout or Finish Review, for a finished review", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("FINISHED", null))),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/no longer available/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /start a new review/i }));
    expect(navigate).toHaveBeenCalledWith("/");
  });

  it("renders a terminal panel, not the diff layout or Finish Review, for an expired review", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("EXPIRED", null))),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/no longer available/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
  });

  it("renders the error panel, not the diff layout, for a conflicted review", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => {
        const doc = reviewDoc("CONFLICTED", null);
        doc.data.attributes.error = {
          code: "CONFLICT",
          message: "#435 conflicts while being applied.",
          change: 435,
          conflictingFiles: ["src/field/FieldService.java"],
        };
        return HttpResponse.json(doc);
      }),
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/#435 conflicts/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /discard review/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /finish review/i })).not.toBeInTheDocument();
  });
});
