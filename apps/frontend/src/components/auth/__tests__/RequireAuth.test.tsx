import { screen } from "@testing-library/react";
import { Route, Routes, useLocation } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/** Renders the location it lands on, so a redirect's target is assertable. */
function LocationProbe() {
  const location = useLocation();
  return <div>login page at {location.pathname + location.search}</div>;
}

function renderGuarded(route: string) {
  return renderWithProviders(
    <Routes>
      <Route path="/login" element={<LocationProbe />} />
      <Route
        path="/reviews/:id"
        element={
          <RequireAuth>
            <div>protected content</div>
          </RequireAuth>
        }
      />
    </Routes>,
    { route },
  );
}

describe("RequireAuth", () => {
  it("redirects an unauthenticated visitor to /login with the attempted path as next", async () => {
    server.use(
      http.get("/api/auth/me", () =>
        HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), { status: 401 }),
      ),
    );
    renderGuarded("/reviews/abc");
    expect(
      await screen.findByText("login page at /login?next=%2Freviews%2Fabc"),
    ).toBeInTheDocument();
  });

  it("renders children for an authenticated user", async () => {
    server.use(
      http.get("/api/auth/me", () =>
        HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        ),
      ),
    );
    renderGuarded("/reviews/abc");
    expect(await screen.findByText("protected content")).toBeInTheDocument();
  });
});
