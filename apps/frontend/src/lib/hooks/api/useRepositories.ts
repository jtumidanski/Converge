import { useQuery } from "@tanstack/react-query";
import { repositoriesService, type RepositoryListParams } from "@/services/api";

export const repositoryKeys = {
  all: ["repositories"] as const,
  lists: () => [...repositoryKeys.all, "list"] as const,
  list: (providerId: string, params?: RepositoryListParams) =>
    [...repositoryKeys.lists(), providerId, params ?? {}] as const,
  details: () => [...repositoryKeys.all, "detail"] as const,
  detail: (providerId: string, fullName: string) =>
    [...repositoryKeys.details(), providerId, fullName] as const,
};

export function useRepositories(providerId: string | undefined, params: RepositoryListParams = {}) {
  return useQuery({
    queryKey: repositoryKeys.list(providerId ?? "", params),
    queryFn: () => repositoriesService.list(providerId as string, params),
    enabled: Boolean(providerId),
    staleTime: 2 * 60_000,
  });
}

export function useRepository(providerId: string | undefined, fullName: string, enabled: boolean) {
  return useQuery({
    queryKey: repositoryKeys.detail(providerId ?? "", fullName),
    queryFn: () => repositoriesService.get(providerId as string, fullName),
    enabled: enabled && Boolean(providerId) && fullName.length > 0,
    retry: false,
  });
}
