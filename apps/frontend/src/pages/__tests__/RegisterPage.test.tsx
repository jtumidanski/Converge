import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { RegisterPage } from "@/pages/RegisterPage";

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

async function fillAndSubmit(username: string, password: string, confirmPassword: string) {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Username"), username);
  await user.type(screen.getByLabelText("Password"), password);
  await user.type(screen.getByLabelText("Confirm password"), confirmPassword);
  await user.click(screen.getByRole("button", { name: "Create account" }));
}

describe("RegisterPage", () => {
  it("submits and lands on provider settings", async () => {
    server.use(
      http.post("/api/auth/register", async ({ request }) => {
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
    renderWithProviders(<RegisterPage />, { route: "/register" });
    await fillAndSubmit("alice", "12345678", "12345678");
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith("/settings/providers", { replace: true }),
    );
  });

  it("shows a confirm-password mismatch on the confirmPassword field", async () => {
    renderWithProviders(<RegisterPage />, { route: "/register" });
    await fillAndSubmit("alice", "12345678", "different");
    expect(await screen.findByText("The passwords do not match.")).toBeInTheDocument();
  });

  it("maps USERNAME_TAKEN onto the username field", async () => {
    server.use(
      http.post("/api/auth/register", () =>
        HttpResponse.json(errorDoc(409, "USERNAME_TAKEN", "That username is taken."), {
          status: 409,
        }),
      ),
    );
    renderWithProviders(<RegisterPage />, { route: "/register" });
    await fillAndSubmit("alice", "12345678", "12345678");
    expect(await screen.findByText("That username is taken.")).toBeInTheDocument();
    expect(screen.getByLabelText("Username")).toHaveAttribute("aria-invalid", "true");
  });

  it("maps WEAK_PASSWORD onto the password field", async () => {
    server.use(
      http.post("/api/auth/register", () =>
        HttpResponse.json(errorDoc(422, "WEAK_PASSWORD", "That password is too weak."), {
          status: 422,
        }),
      ),
    );
    renderWithProviders(<RegisterPage />, { route: "/register" });
    await fillAndSubmit("alice", "12345678", "12345678");
    expect(await screen.findByText("That password is too weak.")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveAttribute("aria-invalid", "true");
  });

  it("maps INVALID_USERNAME onto the username field", async () => {
    server.use(
      http.post("/api/auth/register", () =>
        HttpResponse.json(errorDoc(422, "INVALID_USERNAME", "That username is not allowed."), {
          status: 422,
        }),
      ),
    );
    renderWithProviders(<RegisterPage />, { route: "/register" });
    await fillAndSubmit("alice", "12345678", "12345678");
    expect(await screen.findByText("That username is not allowed.")).toBeInTheDocument();
    expect(screen.getByLabelText("Username")).toHaveAttribute("aria-invalid", "true");
  });
});
