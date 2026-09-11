import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { branchesService, type BranchListParams } from "@/services/api";

export const branchKeys = {
  all: ["branches"] as const,
  lists: () => [...branchKeys.all, "list"] as const,
  list: (providerId: string, repository: string, params?: BranchListParams) =>
    [...branchKeys.lists(), providerId, repository, params ?? {}] as const,
};

/**
 * useBranches backs the base-branch picker. placeholderData keeps the previous
 * result on screen while a new search is in flight, so typing narrows the list
 * instead of flashing it empty; the AbortSignal React Query supplies is passed
 * straight through, which is what cancels a superseded request.
 */
export function useBranches(
  providerId: string | undefined,
  repository: string | undefined,
  params: BranchListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: branchKeys.list(providerId ?? "", repository ?? "", params),
    queryFn: ({ signal }) =>
      branchesService.list(providerId as string, repository as string, params, { signal }),
    enabled: enabled && Boolean(providerId) && Boolean(repository),
    staleTime: 2 * 60_000,
    placeholderData: keepPreviousData,
  });
}
