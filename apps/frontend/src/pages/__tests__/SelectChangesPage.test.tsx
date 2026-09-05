import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { SelectChangesPage } from "@/pages/SelectChangesPage";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate };
});

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
  sessionStorage.clear();
});
afterAll(() => server.close());

function changeDoc(number: number, title: string, author: string) {
  return oneDoc("changes", String(number), {
    number,
    title,
    author,
    sourceBranch: `feat/${number}`,
    targetBranch: "main",
    mergedAt: "2026-08-21T14:02:11Z",
    createdAt: "2026-08-20T09:00:00Z",
    landingSha: "a".repeat(40),
    webUrl: `https://example.test/${number}`,
  }).data;
}

function seed(changes = [changeDoc(421, "Add field-state endpoint", "jsmith"), changeDoc(427, "Fix typo", "mkay")]) {
  server.use(
    http.get("/api/providers/gh/repositories/:repo", () =>
      HttpResponse.json(oneDoc("repositories", "atlas/server", { name: "server", namespace: "atlas", defaultBranch: "main", webUrl: "u" })),
    ),
    http.get("/api/providers/gh/repositories/:repo/changes", ({ request }) => {
      const search = new URL(request.url).searchParams.get("search") ?? "";
      const filtered = search
        ? changes.filter(
            (c) =>
              c.attributes.title.toLowerCase().includes(search.toLowerCase()) ||
              c.attributes.author.toLowerCase().includes(search.toLowerCase()),
          )
        : changes;
      return HttpResponse.json(listDoc(filtered, { number: 1, size: 30, hasNext: false }));
    }),
  );
}

const route = "/select?provider=gh&repo=atlas%2Fserver";

describe("SelectChangesPage", () => {
  it("lists merged changes with their details", async () => {
    seed();
    renderWithProviders(<SelectChangesPage />, { route });
    expect(await screen.findByText("Add field-state endpoint")).toBeInTheDocument();
    const row = screen.getByText("Add field-state endpoint").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("jsmith")).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText(/feat\/421/)).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText(/aaaaaaa/)).toBeInTheDocument();
    expect(await screen.findByDisplayValue("main")).toBeInTheDocument();
  });

  it("keeps selections across a search and enables Build Review", async () => {
    seed();
    renderWithProviders(<SelectChangesPage />, { route });
    await screen.findByText("Add field-state endpoint");
    const build = screen.getByRole("button", { name: /build review/i });
    expect(build).toBeDisabled();
    const firstCheckbox = screen.getAllByRole("checkbox")[0] as HTMLElement;
    await userEvent.click(firstCheckbox);
    expect(build).toBeEnabled();
    expect(screen.getByText(/1 selected/i)).toBeInTheDocument();
    expect(firstCheckbox).toHaveAttribute("aria-checked", "true");
    await userEvent.type(screen.getByPlaceholderText(/search/i), "typo");
    await waitFor(() => expect(screen.queryByText("Add field-state endpoint")).not.toBeInTheDocument());
    expect(screen.getByText(/1 selected/i)).toBeInTheDocument();
    expect(build).toBeEnabled();
  });

  it("creates a review and navigates to it", async () => {
    seed();
    server.use(
      http.post("/api/reviews", async ({ request }) => {
        const body = (await request.json()) as { data: { attributes: { changes: number[]; baseBranch: string } } };
        expect(body.data.attributes.changes).toEqual([421]);
        expect(body.data.attributes.baseBranch).toBe("main");
        return HttpResponse.json(oneDoc("reviews", "7f14b2c8", { status: "CREATING" }), { status: 202 });
      }),
    );
    renderWithProviders(<SelectChangesPage />, { route });
    await screen.findByText("Add field-state endpoint");
    await userEvent.click(screen.getAllByRole("checkbox")[0] as HTMLElement);
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/reviews/7f14b2c8"));
  });

  it("shows the API detail when creating a review fails", async () => {
    seed();
    server.use(
      http.post("/api/reviews", () =>
        HttpResponse.json(
          { errors: [{ status: "400", code: "INCOMPATIBLE_TARGETS", title: "Bad Request", detail: "All selected PRs/MRs must target the same base branch." }] },
          { status: 400 },
        ),
      ),
    );
    renderWithProviders(<SelectChangesPage />, { route });
    await screen.findByText("Add field-state endpoint");
    await userEvent.click(screen.getAllByRole("checkbox")[0] as HTMLElement);
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    expect(await screen.findByText(/same base branch/i)).toBeInTheDocument();
    expect(navigate).not.toHaveBeenCalled();
  });
});
