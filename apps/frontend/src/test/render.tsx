import type { ReactElement, ReactNode } from "react";
import { render, type RenderResult } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { ThemeProvider } from "@/components/theme/ThemeProvider";

function testClient(gcTime = 0): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime }, mutations: { retry: false } },
  });
}

/** A wrapper component that also exposes the QueryClient it renders, so tests can inspect cache state. */
export interface QueryWrapper {
  (props: { children: ReactNode }): ReactElement;
  client: QueryClient;
}

/**
 * queryWrapper wraps hooks under test in a QueryClientProvider.
 *
 * Pass `gcTime: Infinity` when a test needs to inspect cache state (e.g. `isInvalidated`)
 * after a mutation settles, since the default `gcTime: 0` garbage-collects inactive
 * queries almost immediately.
 */
export function queryWrapper(options: { gcTime?: number } = {}): QueryWrapper {
  const client = testClient(options.gcTime ?? 0);
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.client = client;
  return Wrapper;
}

/** renderWithProviders renders a component with router and query providers. */
export function renderWithProviders(
  ui: ReactElement,
  options: { route?: string } = {},
): RenderResult {
  const client = testClient();
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[options.route ?? "/"]}>{ui}</MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}
