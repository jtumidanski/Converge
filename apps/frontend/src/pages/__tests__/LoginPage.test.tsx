import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { LoginPage } from "@/pages/LoginPage";

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

function fillAndSubmit(username: string, password: string) {
  const user = userEvent.setup();
  return (async () => {
    await user.type(screen.getByLabelText("Username"), username);
    await user.type(screen.getByLabelText("Password"), password);
    await user.click(screen.getByRole("button", { name: "Log in" }));
  })();
}

describe("LoginPage", () => {
  it("submits and returns to next", async () => {
    server.use(
      http.post("/api/auth/login", async ({ request }) => {
        const body = (await request.json()) as {
          data: { type: string; attributes: { username: string; password: string } };
        };
        expect(body).toEqual({
          data: {
            type: "credentials",
            attributes: { username: "alice", password: "12345678" },
          },
        });
        return HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        );
      }),
    );
    renderWithProviders(<LoginPage />, { route: "/login?next=%2Freviews%2Fabc" });
    await fillAndSubmit("alice", "12345678");
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/reviews/abc", { replace: true }));
  });

  it("defaults to / when next is absent", async () => {
    server.use(
      http.post("/api/auth/login", () =>
        HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        ),
      ),
    );
    renderWithProviders(<LoginPage />, { route: "/login" });
    await fillAndSubmit("alice", "12345678");
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/", { replace: true }));
  });

  it("rejects a next that is not a local path", async () => {
    server.use(
      http.post("/api/auth/login", () =>
        HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        ),
      ),
    );
    renderWithProviders(<LoginPage />, {
      route: "/login?next=" + encodeURIComponent("https://evil.test/"),
    });
    await fillAndSubmit("alice", "12345678");
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/", { replace: true }));
  });

  it("shows client-side validation before any request", async () => {
    let requestCount = 0;
    server.use(
      http.post("/api/auth/login", () => {
        requestCount += 1;
        return HttpResponse.json(
          oneDoc("users", "u1", { username: "alice", createdAt: "x", providerCount: 0 }),
        );
      }),
    );
    renderWithProviders(<LoginPage />, { route: "/login" });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Log in" }));
    expect(await screen.findByText(/3 to 32 characters/)).toBeInTheDocument();
    expect(screen.getByText("At least 8 characters.")).toBeInTheDocument();
    expect(requestCount).toBe(0);
  });

  it("surfaces INVALID_CREDENTIALS as a form-level error, not attached to a field", async () => {
    server.use(
      http.post("/api/auth/login", () =>
        HttpResponse.json(errorDoc(401, "INVALID_CREDENTIALS", "Incorrect username or password."), {
          status: 401,
        }),
      ),
    );
    renderWithProviders(<LoginPage />, { route: "/login" });
    await fillAndSubmit("alice", "12345678");
    expect(await screen.findByText("Incorrect username or password.")).toBeInTheDocument();
    expect(screen.getByLabelText("Username")).not.toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Password")).not.toHaveAttribute("aria-invalid", "true");
  });

  it("surfaces ACCOUNT_LOCKED", async () => {
    server.use(
      http.post("/api/auth/login", () =>
        HttpResponse.json(
          errorDoc(429, "ACCOUNT_LOCKED", "Too many attempts. Try again in 10 minutes."),
          { status: 429 },
        ),
      ),
    );
    renderWithProviders(<LoginPage />, { route: "/login" });
    await fillAndSubmit("alice", "12345678");
    expect(
      await screen.findByText("Too many attempts. Try again in 10 minutes."),
    ).toBeInTheDocument();
  });

  it("never puts a password in the DOM as plain text", async () => {
    renderWithProviders(<LoginPage />, { route: "/login" });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Password"), "supersecretvalue");
    const passwordInput = screen.getByLabelText("Password");
    expect(passwordInput).toHaveAttribute("type", "password");
    expect(document.body.innerHTML).not.toContain("supersecretvalue");
  });

  it("links to register (FR-8.2)", () => {
    renderWithProviders(<LoginPage />, { route: "/login" });
    expect(screen.getByRole("link", { name: "Register" })).toHaveAttribute("href", "/register");
  });
});
