import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { ReviewsPage } from "@/pages/ReviewsPage";
import { HttpResponse, http, listDoc, server } from "@/test/server";
import { recordRecent } from "@/lib/storage/recents";

function repo(fullName: string) {
  const [namespace = "", name = ""] = fullName.split("/");
  return {
    type: "repositories" as const,
    id: fullName,
    attributes: {
      name,
      namespace,
      defaultBranch: "main",
      webUrl: `https://gitlab.test/${fullName}`,
    },
  };
}

function seed() {
  server.use(
    http.get("/api/reviews", () => HttpResponse.json(listDoc([]))),
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
    http.get("/api/providers/gl/repositories", ({ request }) => {
      const search = new URL(request.url).searchParams.get("search") ?? "";
      const all = [repo("atlas/server"), repo("atlas/client")];
      const items = search === "" ? all : all.filter((r) => r.id.includes(search));
      return HttpResponse.json(listDoc(items, { number: 1, size: 30, hasNext: false }));
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
        <MemoryRouter initialEntries={["/"]}>
          <AppShell>
            <Routes>
              <Route path="/" element={<ReviewsPage />} />
              <Route path="/select" element={<p>select page</p>} />
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
  vi.restoreAllMocks();
});
afterAll(() => server.close());

describe("ReviewsPage", () => {
  it("publishes the Reviews breadcrumb", async () => {
    seed();
    renderPage();
    await waitFor(() =>
      expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toHaveTextContent("Reviews"),
    );
  });

  it("opens the drawer from the start-a-new-review row", async () => {
    seed();
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    expect(await screen.findByRole("dialog", { name: /new review/i })).toBeInTheDocument();
  });

  it("opens the drawer with the n key and closes it with Escape", async () => {
    seed();
    renderPage();
    await screen.findByRole("button", { name: /start a new review/i });
    await userEvent.keyboard("n");
    expect(await screen.findByRole("dialog", { name: /new review/i })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: /new review/i })).not.toBeInTheDocument(),
    );
  });

  it("lists recent repositories first when the search is empty", async () => {
    recordRecent({
      provider: "gl",
      repository: "atlas/client",
      defaultBranch: "main",
      openedAt: "2026-01-01T00:00:00Z",
    });
    seed();
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    const options = await screen.findAllByRole("option");
    expect(options[0]).toHaveTextContent("atlas/client");
  });

  it("searches the provider and enables the primary action on selection", async () => {
    seed();
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    const input = await screen.findByPlaceholderText(/search repositories/i);
    await userEvent.type(input, "server");
    const option = await screen.findByRole("option", { name: /atlas\/server/ });
    await userEvent.click(option);
    const go = screen.getByRole("button", { name: /choose changes/i });
    expect(go).toBeEnabled();
    await userEvent.click(go);
    expect(await screen.findByText("select page")).toBeInTheDocument();
  });

  it("shows an inline error when a pasted name does not resolve", async () => {
    seed();
    server.use(
      http.get("/api/providers/gl/repositories/:repo", () =>
        HttpResponse.json(
          { errors: [{ status: "404", code: "NOT_FOUND", title: "Not Found", detail: "gone" }] },
          { status: 404 },
        ),
      ),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    const input = await screen.findByPlaceholderText(/search repositories/i);
    await userEvent.type(input, "atlas/missing{Enter}");
    expect(await screen.findByText(/could not be found/i)).toBeInTheDocument();
  });
});
