import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { errorDoc, http, HttpResponse, listDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { ProviderSettingsPage } from "@/pages/ProviderSettingsPage";

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function providerResource(id: string, overrides: Record<string, unknown> = {}) {
  return {
    type: "userProviders",
    id,
    attributes: {
      slug: "gh-main",
      displayName: "Main GitHub",
      kind: "github",
      baseUrl: "",
      tokenLast4: "9f2c",
      tokenSetAt: "2026-01-01T00:00:00Z",
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      ...overrides,
    },
  };
}

describe("ProviderSettingsPage", () => {
  it("lists providers with kind, display name, base URL and a masked token", async () => {
    server.use(
      http.get("/api/settings/providers", () =>
        HttpResponse.json(listDoc([providerResource("p1")])),
      ),
    );
    renderWithProviders(<ProviderSettingsPage />, { route: "/settings/providers" });

    expect(await screen.findByText("Main GitHub")).toBeInTheDocument();
    expect(screen.getByText("GitHub")).toBeInTheDocument();
    expect(screen.getByText("•••• 9f2c")).toBeInTheDocument();
  });

  it("never renders a stored token", async () => {
    server.use(
      http.get("/api/settings/providers", () =>
        HttpResponse.json(listDoc([providerResource("p1")])),
      ),
    );
    const { container } = renderWithProviders(<ProviderSettingsPage />, {
      route: "/settings/providers",
    });

    await screen.findByText("Main GitHub");
    // The server never sends a token; the fixture's tokenLast4 is the only
    // token-shaped text that may legitimately appear.
    expect(container.innerHTML).not.toMatch(/ghp_[a-zA-Z0-9]+/);
    expect(container.innerHTML.match(/9f2c/g)?.length).toBe(1);
  });

  it("shows an empty state that points at adding a provider", async () => {
    server.use(http.get("/api/settings/providers", () => HttpResponse.json(listDoc([]))));
    renderWithProviders(<ProviderSettingsPage />, { route: "/settings/providers" });

    expect(await screen.findByText("No providers yet")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add provider" })).toBeInTheDocument();
  });

  it("deletes a provider", async () => {
    let listCalls = 0;
    server.use(
      http.get("/api/settings/providers", () => {
        listCalls += 1;
        return HttpResponse.json(listDoc(listCalls === 1 ? [providerResource("p1")] : []));
      }),
      http.delete("/api/settings/providers/p1", () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<ProviderSettingsPage />, { route: "/settings/providers" });

    await screen.findByText("Main GitHub");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Delete Main GitHub" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete" }));

    await waitFor(() => expect(screen.getByText("No providers yet")).toBeInTheDocument());
    expect(listCalls).toBeGreaterThanOrEqual(2);
  });

  it("surfaces PROVIDER_IN_USE on delete", async () => {
    server.use(
      http.get("/api/settings/providers", () =>
        HttpResponse.json(listDoc([providerResource("p1")])),
      ),
      http.delete("/api/settings/providers/p1", () =>
        HttpResponse.json(errorDoc(409, "PROVIDER_IN_USE", "This provider is used by a review."), {
          status: 409,
        }),
      ),
    );
    renderWithProviders(<ProviderSettingsPage />, { route: "/settings/providers" });

    await screen.findByText("Main GitHub");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Delete Main GitHub" }));
    await user.click(screen.getByRole("button", { name: "Confirm delete" }));

    expect(await screen.findByText("This provider is used by a review.")).toBeInTheDocument();
    expect(screen.getByText("Main GitHub")).toBeInTheDocument();
  });

  it("opens the edit form for a row and hides the slug input there", async () => {
    server.use(
      http.get("/api/settings/providers", () =>
        HttpResponse.json(listDoc([providerResource("p1")])),
      ),
    );
    renderWithProviders(<ProviderSettingsPage />, { route: "/settings/providers" });

    await screen.findByText("Main GitHub");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Edit Main GitHub" }));

    const form = screen.getByRole("button", { name: "Save changes" }).closest("form");
    expect(form).not.toBeNull();
    expect(within(form as HTMLElement).queryByLabelText("Slug")).not.toBeInTheDocument();
    expect(within(form as HTMLElement).getByText("gh-main")).toBeInTheDocument();
  });
});
