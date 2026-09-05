import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { changesService, type ChangeListParams } from "@/services/api";

export const changeKeys = {
  all: ["changes"] as const,
  lists: () => [...changeKeys.all, "list"] as const,
  list: (providerId: string, repository: string, params: ChangeListParams) =>
    [...changeKeys.lists(), providerId, repository, params] as const,
};

export function useChanges(
  providerId: string | undefined,
  repository: string | undefined,
  params: ChangeListParams,
) {
  return useQuery({
    queryKey: changeKeys.list(providerId ?? "", repository ?? "", params),
    queryFn: () => changesService.list(providerId as string, repository as string, params),
    enabled: Boolean(providerId) && Boolean(repository),
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  });
}
