import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { AppRoutes } from "@/routes";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderAt(route: string, hosted: boolean) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[route]}>
          <AppRoutes hosted={hosted} />
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

describe("AppRoutes", () => {
  it("renders the not-found panel for an unmatched path", () => {
    renderAt("/this-does-not-exist", false);
    expect(screen.getByText("Page not found")).toBeInTheDocument();
  });

  it("routes /reviews/:id to the review page rather than the not-found panel", () => {
    const { container } = renderAt("/reviews/7f14b2c8", false);
    expect(screen.queryByText("Page not found")).not.toBeInTheDocument();
    // Before the review data loads, ReviewPage renders a loading skeleton.
    expect(container.querySelector('[data-slot="skeleton"]')).toBeInTheDocument();
  });

  describe("standalone", () => {
    it("excludes the settings and auth paths", () => {
      for (const path of ["/settings/providers", "/settings/account", "/login", "/register"]) {
        const { unmount } = renderAt(path, false);
        expect(screen.getByText("Page not found")).toBeInTheDocument();
        unmount();
      }
    });
  });

  describe("hosted", () => {
    it("includes the login and register paths", () => {
      const login = renderAt("/login", true);
      expect(screen.getByText("Log in")).toBeInTheDocument();
      login.unmount();

      const register = renderAt("/register", true);
      expect(screen.getByText("Create an account")).toBeInTheDocument();
      register.unmount();
    });

    it("guards the settings paths with RequireAuth, which redirects an unauthenticated visitor to /login", async () => {
      server.use(
        http.get("/api/auth/me", () =>
          HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), { status: 401 }),
        ),
      );
      for (const path of ["/settings/providers", "/settings/account"]) {
        const { unmount } = renderAt(path, true);
        expect(await screen.findByText("Log in")).toBeInTheDocument();
        expect(screen.queryByText("Page not found")).not.toBeInTheDocument();
        unmount();
      }
    });

    it("renders the settings pages for an authenticated visitor", async () => {
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
      renderAt("/settings/providers", true);
      expect(await screen.findByText("Provider settings")).toBeInTheDocument();
    });
  });
});
