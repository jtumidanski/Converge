import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { SelectChangesPage } from "@/pages/SelectChangesPage";
import { HttpResponse, http, listDoc, oneDoc, server } from "@/test/server";
import { RECENTS_KEY } from "@/lib/storage/recents";

function change(number: number, title: string, sourceBranch: string, author = "jsmith") {
  return {
    type: "changes" as const,
    id: String(number),
    attributes: {
      number,
      title,
      author,
      sourceBranch,
      targetBranch: "main",
      mergedAt: `2026-01-0${number}T00:00:00Z`,
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: `https://example.test/mr/${number}`,
    },
  };
}

let changeQueries: string[] = [];
let createdBody: unknown = null;

function seed() {
  changeQueries = [];
  createdBody = null;
  server.use(
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
    http.get("/api/providers/gl/repositories/:repo/branches", () =>
      HttpResponse.json(
        listDoc([
          {
            type: "branches" as const,
            id: "gl:atlas/server:main",
            attributes: { name: "main", isDefault: true, sha: "" },
          },
          {
            type: "branches" as const,
            id: "gl:atlas/server:develop",
            attributes: { name: "develop", isDefault: false, sha: "" },
          },
        ]),
      ),
    ),
    http.get("/api/providers/gl/repositories/:repo/changes", ({ request }) => {
      changeQueries.push(new URL(request.url).searchParams.get("target") ?? "");
      return HttpResponse.json(
        listDoc(
          [
            change(1, "ATLAS-1 first", "feat/a"),
            change(2, "ATLAS-1 second", "feat/b", "zoe"),
            change(3, "chore(deps): bump lodash", "renovate/lodash-4.x"),
          ],
          { number: 1, size: 30, hasNext: false },
        ),
      );
    }),
    http.post("/api/reviews", async ({ request }) => {
      createdBody = await request.json();
      return HttpResponse.json(
        oneDoc("reviews", "rev-1", {
          status: "CREATING",
          stage: null,
          provider: "gl",
          repository: "atlas/server",
          baseBranch: "main",
          baseSha: null,
          headSha: null,
          baseDescription: "",
          changes: [1, 2],
          included: [],
          totals: null,
          error: null,
          createdAt: "2026-01-01T00:00:00Z",
          updatedAt: "2026-01-01T00:00:00Z",
          expiresAt: "2999-01-01T00:00:00Z",
        }),
        { status: 201 },
      );
    }),
  );
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/select?provider=gl&repo=atlas%2Fserver"]}>
          <AppShell>
            <Routes>
              <Route path="/select" element={<SelectChangesPage />} />
              <Route path="/reviews/:id" element={<p>review page</p>} />
            </Routes>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  localStorage.clear();
  sessionStorage.clear();
});
afterAll(() => server.close());

describe("SelectChangesPage", () => {
  it("publishes the repository breadcrumb", async () => {
    seed();
    renderPage();
    await waitFor(() =>
      expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toHaveTextContent(
        "atlas/server",
      ),
    );
  });

  it("records the repository as recent on load", async () => {
    seed();
    renderPage();
    await waitFor(() => {
      const stored = JSON.parse(localStorage.getItem(RECENTS_KEY) ?? "[]") as Array<{
        repository: string;
      }>;
      expect(stored[0]?.repository).toBe("atlas/server");
    });
  });

  it("hides dependency-bot changes by default and can show them", async () => {
    seed();
    renderPage();
    expect(await screen.findByText("ATLAS-1 first")).toBeInTheDocument();
    expect(screen.queryByText(/bump lodash/)).not.toBeInTheDocument();
    expect(screen.getByText(/1 changes hidden by/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(await screen.findByText(/bump lodash/)).toBeInTheDocument();
  });

  it("filters by author chip", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByRole("button", { name: "zoe" }));
    expect(screen.queryByText("ATLAS-1 first")).not.toBeInTheDocument();
    expect(screen.getByText("ATLAS-1 second")).toBeInTheDocument();
  });

  it("groups by ticket and selects a whole group", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByRole("button", { name: /group by ticket/i }));
    await userEvent.click(await screen.findByRole("button", { name: "Select all 2" }));
    expect(screen.getByText("2 selected · applied oldest → newest")).toBeInTheDocument();
  });

  it("refetches with the new target when the base changes", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.click(await screen.findByRole("option", { name: /develop/ }));
    await waitFor(() => expect(changeQueries).toContain("develop"));
  });

  it("posts the selected changes in apply order", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 second");
    await userEvent.click(screen.getByText("ATLAS-1 second"));
    await userEvent.click(screen.getByText("ATLAS-1 first"));
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    await waitFor(() => expect(createdBody).not.toBeNull());
    // reviewsService.create wraps the request in a JSON:API envelope
    // (data.attributes), not a bare body — the brief's sample asserted on
    // createdBody.changes directly, which reviewsService never produces.
    expect(
      (createdBody as { data: { attributes: { changes: number[] } } }).data.attributes.changes,
    ).toEqual([1, 2]);
    expect(await screen.findByText("review page")).toBeInTheDocument();
  });

  it("shows the API detail when creating a review fails", async () => {
    seed();
    server.use(
      http.post("/api/reviews", () =>
        HttpResponse.json(
          {
            errors: [
              {
                status: "400",
                code: "INCOMPATIBLE_TARGETS",
                title: "Bad Request",
                detail: "All selected PRs/MRs must target the same base branch.",
              },
            ],
          },
          { status: 400 },
        ),
      ),
    );
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByText("ATLAS-1 first"));
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    expect(await screen.findByText(/same base branch/i)).toBeInTheDocument();
    expect(screen.queryByText("review page")).not.toBeInTheDocument();
  });

  it("shows a banner when the branches fetch fails, without silently falling back", async () => {
    seed();
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(
          { errors: [{ status: "500", code: "GIT_FAILURE", title: "Server Error" }] },
          { status: 500 },
        ),
      ),
    );
    renderPage();
    expect(await screen.findByText("Could not load branches")).toBeInTheDocument();
  });

  it("waits for the repository lookup to settle before requesting changes, and targets the default branch", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    expect(changeQueries).toHaveLength(1);
    expect(changeQueries[0]).toBe("main");
  });

  it("still fetches and renders changes untargeted when the repository lookup fails", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo", () =>
        HttpResponse.json(
          { errors: [{ status: "404", code: "NOT_FOUND", title: "Not Found" }] },
          { status: 404 },
        ),
      ),
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([])),
      ),
      http.get("/api/providers/gl/repositories/:repo/changes", ({ request }) => {
        changeQueries.push(new URL(request.url).searchParams.get("target") ?? "");
        expect(new URL(request.url).searchParams.get("target")).toBeNull();
        return HttpResponse.json(
          listDoc([change(1, "ATLAS-1 first", "feat/a")], { number: 1, size: 30, hasNext: false }),
        );
      }),
    );
    renderPage();
    expect(await screen.findByText("ATLAS-1 first")).toBeInTheDocument();
  });

  it("shows a guidance banner instead of fetching when the provider or repository is missing from the URL", () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <ThemeProvider>
          <MemoryRouter initialEntries={["/select?provider=gl"]}>
            <AppShell>
              <Routes>
                <Route path="/select" element={<SelectChangesPage />} />
              </Routes>
            </AppShell>
          </MemoryRouter>
        </ThemeProvider>
      </QueryClientProvider>,
    );
    expect(screen.getByText(/missing selection/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("wires Pagination to page state and gates Next on hasNext", async () => {
    const pagesRequested: string[] = [];
    server.use(
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
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([])),
      ),
      http.get("/api/providers/gl/repositories/:repo/changes", ({ request }) => {
        const url = new URL(request.url);
        pagesRequested.push(url.searchParams.get("page") ?? "1");
        return HttpResponse.json(
          listDoc([change(1, "ATLAS-1 first", "feat/a")], { number: 1, size: 30, hasNext: true }),
        );
      }),
    );
    renderPage();
    await screen.findByText("ATLAS-1 first");
    const next = screen.getByRole("button", { name: /next/i });
    expect(next).toBeEnabled();
    await userEvent.click(next);
    await waitFor(() => expect(pagesRequested).toContain("2"));
    expect(screen.getByText(/page 2/i)).toBeInTheDocument();
    const previous = screen.getByRole("button", { name: /previous/i });
    expect(previous).toBeEnabled();
    await userEvent.click(previous);
    await waitFor(() => expect(screen.getByText(/page 1/i)).toBeInTheDocument());
    expect(previous).toBeDisabled();
  });
});
