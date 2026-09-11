import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "@/components/auth/AuthProvider";
import { errorDoc, http, HttpResponse, server } from "@/test/server";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// useNavigate is mocked directly rather than asserted on via rendered route
// content: navigating to the *same* matched route with only the query string
// changed does not always produce a new DOM node to assert against, but the
// call itself is exactly what the handler's behaviour (redirect vs no-op)
// hinges on.
const { navigateSpy } = vi.hoisted(() => ({ navigateSpy: vi.fn() }));

vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigateSpy };
});

function renderApp(route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>
        <AuthProvider>
          <div>content</div>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

describe("AuthProvider", () => {
  it("redirects to /login and clears the query cache on a 401 from any call", async () => {
    navigateSpy.mockClear();
    let providersCallCount = 0;
    server.use(
      http.get("/api/settings/providers", () => {
        providersCallCount += 1;
        return HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Session expired"), {
          status: 401,
        });
      }),
    );
    const client = renderApp("/reviews/abc");
    client.setQueryData(["stale", "entry"], { stale: true });
    expect(await screen.findByText("content")).toBeInTheDocument();

    // Simulate an arbitrary authenticated call answering 401 mid-session.
    const { apiGet } = await import("@/lib/api/client");
    await expect(apiGet("/api/settings/providers")).rejects.toThrow();
    expect(providersCallCount).toBe(1);

    await vi.waitFor(() => expect(navigateSpy).toHaveBeenCalledTimes(1));
    expect(navigateSpy).toHaveBeenCalledWith("/login?next=%2Freviews%2Fabc", { replace: true });
    expect(client.getQueryData(["stale", "entry"])).toBeUndefined();
  });

  it("does not redirect when already on /login, since the 401 from /api/auth/me there is expected", async () => {
    navigateSpy.mockClear();
    server.use(
      http.get("/api/auth/me", () =>
        HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), { status: 401 }),
      ),
    );
    renderApp("/login");
    expect(await screen.findByText("content")).toBeInTheDocument();

    const { apiGet } = await import("@/lib/api/client");
    await expect(apiGet("/api/auth/me")).rejects.toThrow();

    // Give any (incorrect) redirect a chance to happen before asserting it
    // did not.
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(navigateSpy).not.toHaveBeenCalled();
  });
});
