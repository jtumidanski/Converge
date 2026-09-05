import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { AppRoutes } from "@/routes";

function renderAt(route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>
        <AppRoutes />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("AppRoutes", () => {
  it("renders the not-found panel for an unmatched path", () => {
    renderAt("/this-does-not-exist");
    expect(screen.getByText("Page not found")).toBeInTheDocument();
  });

  it("routes /reviews/:id to the review page rather than the not-found panel", () => {
    renderAt("/reviews/7f14b2c8");
    expect(screen.queryByText("Page not found")).not.toBeInTheDocument();
    expect(screen.getByText(/loading review/i)).toBeInTheDocument();
  });
});
