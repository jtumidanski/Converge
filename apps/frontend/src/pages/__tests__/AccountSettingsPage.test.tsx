import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { Toaster } from "sonner";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { AccountSettingsPage } from "@/pages/AccountSettingsPage";

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

function mockCurrentUser(overrides: Record<string, unknown> = {}) {
  server.use(
    http.get("/api/auth/me", () =>
      HttpResponse.json(
        oneDoc("users", "u1", {
          username: "alice",
          createdAt: "2026-01-01T00:00:00Z",
          providerCount: 1,
          ...overrides,
        }),
      ),
    ),
  );
}

function renderPage() {
  return renderWithProviders(
    <>
      <AccountSettingsPage />
      <Toaster />
    </>,
    { route: "/settings/account" },
  );
}

/**
 * renderPageWithClient exposes the QueryClient so a test can seed and inspect
 * cache state, which renderWithProviders (by design) does not expose.
 */
function renderPageWithClient() {
  const client = new QueryClient({
    // gcTime: Infinity so the seeded cache entry in the "deletion navigates
    // to /login" test isn't garbage-collected on its own -- only an explicit
    // clear() should remove it.
    defaultOptions: { queries: { retry: false, gcTime: Infinity }, mutations: { retry: false } },
  });
  const utils = render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/settings/account"]}>
          <AccountSettingsPage />
          <Toaster />
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return { ...utils, client };
}

async function fillDeleteForm(password: string, confirmUsername: string) {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Password"), password);
  await user.type(screen.getByLabelText("Type alice to confirm"), confirmUsername);
  return user;
}

describe("AccountSettingsPage", () => {
  it("renders the username and created date", async () => {
    mockCurrentUser();
    renderPage();

    expect(await screen.findByText("alice")).toBeInTheDocument();
    expect(
      screen.getByText(new Date("2026-01-01T00:00:00Z").toLocaleDateString()),
    ).toBeInTheDocument();
  });

  it('all three password inputs and the delete password are type="password"', async () => {
    mockCurrentUser();
    renderPage();
    await screen.findByText("alice");

    expect(screen.getByLabelText("Current password")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("New password")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("Confirm new password")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("Password")).toHaveAttribute("type", "password");
  });

  it("changes the password", async () => {
    mockCurrentUser();
    let body: unknown;
    server.use(
      http.post("/api/auth/password", async ({ request }) => {
        body = await request.json();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();
    await screen.findByText("alice");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Current password"), "oldpassword1");
    await user.type(screen.getByLabelText("New password"), "newpassword1");
    await user.type(screen.getByLabelText("Confirm new password"), "newpassword1");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    await waitFor(() =>
      expect(body).toEqual({
        data: {
          type: "passwords",
          attributes: { currentPassword: "oldpassword1", newPassword: "newpassword1" },
        },
      }),
    );
    expect(await screen.findByText("Password changed.")).toBeInTheDocument();
  });

  it("shows a confirm mismatch before any request", async () => {
    mockCurrentUser();
    renderPage();
    await screen.findByText("alice");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Current password"), "oldpassword1");
    await user.type(screen.getByLabelText("New password"), "newpassword1");
    await user.type(screen.getByLabelText("Confirm new password"), "different1");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByText("The passwords do not match.")).toBeInTheDocument();
  });

  it("maps INVALID_CREDENTIALS onto the current-password field and does not log the user out", async () => {
    mockCurrentUser();
    server.use(
      http.post("/api/auth/password", () =>
        HttpResponse.json(errorDoc(401, "INVALID_CREDENTIALS", "That password is incorrect."), {
          status: 401,
        }),
      ),
    );
    renderPage();
    await screen.findByText("alice");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Current password"), "wrongpassword");
    await user.type(screen.getByLabelText("New password"), "newpassword1");
    await user.type(screen.getByLabelText("Confirm new password"), "newpassword1");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByText("That password is incorrect.")).toBeInTheDocument();
    expect(screen.getByLabelText("Current password")).toHaveAttribute("aria-invalid", "true");
    // The 401 here carries INVALID_CREDENTIALS, not UNAUTHENTICATED: the
    // session stays alive and the signed-in user's data is still on screen.
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(navigate).not.toHaveBeenCalled();
  });

  it("maps WEAK_PASSWORD onto the new-password field", async () => {
    mockCurrentUser();
    server.use(
      http.post("/api/auth/password", () =>
        HttpResponse.json(errorDoc(422, "WEAK_PASSWORD", "That password is too weak."), {
          status: 422,
        }),
      ),
    );
    renderPage();
    await screen.findByText("alice");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Current password"), "oldpassword1");
    await user.type(screen.getByLabelText("New password"), "newpassword1");
    await user.type(screen.getByLabelText("Confirm new password"), "newpassword1");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByText("That password is too weak.")).toBeInTheDocument();
    expect(screen.getByLabelText("New password")).toHaveAttribute("aria-invalid", "true");
  });

  it("deletes the account behind a typed username confirmation", async () => {
    mockCurrentUser();
    renderPage();
    await screen.findByText("alice");

    const deleteButton = screen.getByRole("button", { name: "Delete account" });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Password"), "correctpassword");
    expect(deleteButton).toBeDisabled();

    const confirmField = screen.getByLabelText("Type alice to confirm");
    await user.type(confirmField, "alic");
    expect(deleteButton).toBeDisabled();

    await user.clear(confirmField);
    await user.type(confirmField, "ALICE");
    expect(deleteButton).toBeDisabled();

    await user.clear(confirmField);
    await user.type(confirmField, "alice");
    await waitFor(() => expect(deleteButton).toBeEnabled());
  });

  it("deletion navigates to /login", async () => {
    mockCurrentUser();
    server.use(http.delete("/api/auth/me", () => new HttpResponse(null, { status: 204 })));
    const { client } = renderPageWithClient();
    await screen.findByText("alice");
    // Seed an unrelated cache entry to prove clear() empties more than the
    // auth.me entry alone.
    client.setQueryData(["unrelated", "key"], { value: 1 });

    await fillDeleteForm("correctpassword", "alice");
    const deleteButton = screen.getByRole("button", { name: "Delete account" });
    await waitFor(() => expect(deleteButton).toBeEnabled());
    const user = userEvent.setup();
    await user.click(deleteButton);

    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/login", { replace: true }));
    expect(client.getQueryData(["unrelated", "key"])).toBeUndefined();
  });

  it("maps INVALID_CREDENTIALS on delete onto the password field", async () => {
    mockCurrentUser();
    server.use(
      http.delete("/api/auth/me", () =>
        HttpResponse.json(errorDoc(401, "INVALID_CREDENTIALS", "That password is incorrect."), {
          status: 401,
        }),
      ),
    );
    renderPage();
    await screen.findByText("alice");

    await fillDeleteForm("wrongpassword", "alice");
    const deleteButton = screen.getByRole("button", { name: "Delete account" });
    await waitFor(() => expect(deleteButton).toBeEnabled());
    const user = userEvent.setup();
    await user.click(deleteButton);

    expect(await screen.findByText("That password is incorrect.")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveAttribute("aria-invalid", "true");
    expect(navigate).not.toHaveBeenCalled();
  });
});
