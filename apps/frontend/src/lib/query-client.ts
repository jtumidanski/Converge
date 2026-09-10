import { QueryClient } from "@tanstack/react-query";

/** createQueryClient builds the app's QueryClient with Converge defaults. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 1,
        refetchOnWindowFocus: false,
        staleTime: 60_000,
      },
    },
  });
}
