import type { ReactElement, ReactNode } from "react";
import { render, type RenderResult } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

function testClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
}

/** queryWrapper wraps hooks under test in a QueryClientProvider. */
export function queryWrapper() {
  const client = testClient();
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

/** renderWithProviders renders a component with router and query providers. */
export function renderWithProviders(
  ui: ReactElement,
  options: { route?: string } = {},
): RenderResult {
  const client = testClient();
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[options.route ?? "/"]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}
