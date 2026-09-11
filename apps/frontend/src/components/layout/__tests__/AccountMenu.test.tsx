import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { AccountMenu } from "@/components/layout/AccountMenu";
import { oneDoc, http, HttpResponse, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function LocationProbe() {
  const location = useLocation();
  return <div>at {location.pathname}</div>;
}

function renderMenu(route = "/") {
  return renderWithProviders(
    <Routes>
      <Route path="/login" element={<LocationProbe />} />
      <Route path="*" element={<AccountMenu />} />
    </Routes>,
    { route },
  );
}

function mockCurrentUser() {
  server.use(
    http.get("/api/auth/me", () =>
      HttpResponse.json(
        oneDoc("users", "u1", {
          username: "alice",
          createdAt: "2026-01-01T00:00:00Z",
          providerCount: 1,
        }),
      ),
    ),
  );
}

describe("AccountMenu", () => {
  it("shows the username and the three items", async () => {
    mockCurrentUser();
    renderMenu();
    const trigger = await screen.findByRole("button", { name: "alice" });
    await userEvent.click(trigger);
    expect(screen.getByRole("menuitem", { name: "Provider settings" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Account settings" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Log out" })).toBeInTheDocument();
  });

  it("logout navigates to /login", async () => {
    mockCurrentUser();
    server.use(http.post("/api/auth/logout", () => new HttpResponse(null, { status: 204 })));
    renderMenu();
    const trigger = await screen.findByRole("button", { name: "alice" });
    await userEvent.click(trigger);
    await userEvent.click(screen.getByRole("menuitem", { name: "Log out" }));
    expect(await screen.findByText("at /login")).toBeInTheDocument();
  });
});
