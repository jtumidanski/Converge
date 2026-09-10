import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { AppRoutes } from "@/routes";
import { ThemeProvider } from "@/components/theme/ThemeProvider";

function renderAt(route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[route]}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

describe("AppRoutes", () => {
  it("renders the not-found panel for an unmatched path", () => {
    renderAt("/this-does-not-exist");
    expect(screen.getByText("Page not found")).toBeInTheDocument();
  });

  it("routes /reviews/:id to the review page rather than the not-found panel", () => {
    const { container } = renderAt("/reviews/7f14b2c8");
    expect(screen.queryByText("Page not found")).not.toBeInTheDocument();
    // Before the review data loads, ReviewPage renders a loading skeleton.
    expect(container.querySelector('[data-slot="skeleton"]')).toBeInTheDocument();
  });
});
