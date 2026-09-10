import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { errorDoc, http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate };
});

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
});
afterAll(() => server.close());

function seedProviders() {
  server.use(
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          oneDoc("providers", "gitlab-work", {
            displayName: "GitLab Work",
            kind: "gitlab",
            baseUrl: "https://gitlab.test",
          }).data,
        ]),
      ),
    ),
  );
}

function reviewResource(id: string, overrides: Record<string, unknown> = {}) {
  return oneDoc("reviews", id, {
    status: "READY",
    stage: null,
    provider: "gitlab-work",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: "a".repeat(40),
    headSha: "b".repeat(40),
    baseDescription: "Immediately before #12",
    changes: [12, 14, 15],
    included: [],
    totals: { files: 5, additions: 84, deletions: 12 },
    error: null,
    createdAt: new Date(Date.now() - 12 * 60_000).toISOString(),
    updatedAt: new Date(Date.now() - 60_000).toISOString(),
    expiresAt: new Date(Date.now() + 5 * 3_600_000).toISOString(),
    ...overrides,
  }).data;
}

function seedReviews(...resources: ReturnType<typeof reviewResource>[]) {
  server.use(http.get("/api/reviews", () => HttpResponse.json(listDoc(resources))));
}

describe("SelectRepositoryPage", () => {
  it("shows a skeleton, then repositories for the only provider", async () => {
    seedProviders();
    seedReviews();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          listDoc(
            [
              oneDoc("repositories", "atlas/server", {
                name: "server",
                namespace: "atlas",
                defaultBranch: "main",
                webUrl: "u",
              }).data,
            ],
            { number: 1, size: 30, hasNext: false },
          ),
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText("main")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /select/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalled());
    expect(String(navigate.mock.calls[0]?.[0])).toContain("provider=gitlab-work");
    expect(String(navigate.mock.calls[0]?.[0])).toContain("repo=atlas%2Fserver");
  });

  it("surfaces provider failures in an error banner", async () => {
    seedReviews();
    server.use(
      http.get("/api/providers", () =>
        HttpResponse.json(
          {
            errors: [
              {
                status: "502",
                code: "PROVIDER_AUTH",
                title: "Bad Gateway",
                detail: "The token was rejected.",
              },
            ],
          },
          { status: 502 },
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/token was rejected/i)).toBeInTheDocument();
  });

  it("renders an empty state when no repositories come back", async () => {
    seedProviders();
    seedReviews();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(listDoc([], { number: 1, size: 30, hasNext: false })),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/no repositories/i)).toBeInTheDocument();
  });

  it("surfaces repository failures in an error banner rather than an empty state", async () => {
    seedProviders();
    seedReviews();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          {
            errors: [
              {
                status: "REPOSITORY_UNAVAILABLE",
                code: "REPOSITORY_UNAVAILABLE",
                title: "Repository Unavailable",
                detail: "The repository host did not respond.",
              },
            ],
          },
          { status: 503 },
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/repository host did not respond/i)).toBeInTheDocument();
    expect(screen.queryByText(/no repositories/i)).not.toBeInTheDocument();
  });

  // A disabled React Query reports isLoading=false with data=undefined, so a
  // component that only checks isLoading renders its empty state having never
  // issued a request. Until a provider is chosen the repositories query is
  // disabled, and "This token cannot see any repositories" is a false claim.
  it("does not claim the token sees no repositories before a provider is known", () => {
    seedProviders();
    seedReviews();
    renderWithProviders(<SelectRepositoryPage />);
    expect(screen.queryByText(/cannot see any repositories/i)).not.toBeInTheDocument();
  });

  it("does not claim the token sees no repositories when no provider is configured", async () => {
    seedReviews();
    server.use(http.get("/api/providers", () => HttpResponse.json(listDoc([]))));
    renderWithProviders(<SelectRepositoryPage />);
    // The picker's label replaces its skeleton once the providers query
    // settles, so this waits for the steady state rather than a first paint.
    expect(await screen.findByText("Provider")).toBeInTheDocument();
    expect(screen.queryByText(/cannot see any repositories/i)).not.toBeInTheDocument();
  });

  it("retries the repository fetch when Try again is pressed after a failure", async () => {
    seedProviders();
    seedReviews();
    let calls = 0;
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () => {
        calls += 1;
        if (calls === 1) {
          return HttpResponse.json(
            {
              errors: [
                {
                  status: "503",
                  code: "REPOSITORY_UNAVAILABLE",
                  title: "Repository Unavailable",
                  detail: "The repository host did not respond.",
                },
              ],
            },
            { status: 503 },
          );
        }
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
            { number: 1, size: 30, hasNext: false },
          ),
        );
      }),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/repository host did not respond/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    expect(calls).toBe(2);
  });
});

describe("SelectRepositoryPage resume section", () => {
  it("lists active reviews in the order the server returns them, above the provider picker", async () => {
    seedProviders();
    seedReviews(reviewResource("r1"), reviewResource("r2", { repository: "web/ui" }));
    renderWithProviders(<SelectRepositoryPage />);
    const rows = await screen.findAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("atlas/server");
    expect(rows[1]).toHaveTextContent("web/ui");
    const heading = screen.getByRole("heading", { name: /resume a review/i });
    const picker = screen.getByText("Provider");
    expect(heading.compareDocumentPosition(picker) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("navigates to the review when Resume is pressed", async () => {
    seedProviders();
    seedReviews(reviewResource("r1"));
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /resume review of atlas\/server/i }),
    );
    expect(navigate).toHaveBeenCalledWith("/reviews/r1");
  });

  it("removes the row after a confirmed discard succeeds", async () => {
    seedProviders();
    let discarded = false;
    server.use(
      http.get("/api/reviews", () =>
        HttpResponse.json(listDoc(discarded ? [] : [reviewResource("r1")])),
      ),
      http.delete("/api/reviews/r1", () => {
        discarded = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /discard review of atlas\/server/i }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^discard$/i }));
    expect(await screen.findByText(/no reviews in progress/i)).toBeInTheDocument();
  });

  it("issues no request when the discard confirmation is cancelled", async () => {
    seedProviders();
    let deletes = 0;
    server.use(
      http.get("/api/reviews", () => HttpResponse.json(listDoc([reviewResource("r1")]))),
      http.delete("/api/reviews/r1", () => {
        deletes += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /discard review of atlas\/server/i }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(deletes).toBe(0);
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
  });

  it("keeps the row when the discard fails", async () => {
    seedProviders();
    server.use(
      http.get("/api/reviews", () => HttpResponse.json(listDoc([reviewResource("r1")]))),
      http.delete("/api/reviews/r1", () =>
        HttpResponse.json(errorDoc(500, "INTERNAL", "Internal", "The worktree is locked."), {
          status: 500,
        }),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /discard review of atlas\/server/i }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^discard$/i }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /discard review of atlas\/server/i }),
      ).toBeEnabled(),
    );
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
  });

  it("keeps the repository flow working when the review list fails to load", async () => {
    seedProviders();
    server.use(
      http.get("/api/reviews", () =>
        HttpResponse.json(errorDoc(503, "UNAVAILABLE", "Unavailable", "Sessions are unreadable."), {
          status: 503,
        }),
      ),
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          listDoc(
            [
              oneDoc("repositories", "atlas/server", {
                name: "server",
                namespace: "atlas",
                defaultBranch: "main",
                webUrl: "u",
              }).data,
            ],
            { number: 1, size: 30, hasNext: false },
          ),
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/sessions are unreadable/i)).toBeInTheDocument();
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /select/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalled());
  });

  it("renders the empty state when there are no active reviews", async () => {
    seedProviders();
    seedReviews();
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/no reviews in progress/i)).toBeInTheDocument();
  });
});
