import { useQuery } from "@tanstack/react-query";
import { providersService } from "@/services/api";

export const providerKeys = {
  all: ["providers"] as const,
  lists: () => [...providerKeys.all, "list"] as const,
};

export function useProviders() {
  return useQuery({
    queryKey: providerKeys.lists(),
    queryFn: () => providersService.list(),
    staleTime: 5 * 60_000,
  });
}
