import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { userProvidersService } from "@/services/api";
import type { UserProviderCreate, UserProviderPatch } from "@/services/api/userProviders";
import { providerKeys } from "@/lib/hooks/api/useProviders";

export const userProviderKeys = {
  all: ["userProviders"] as const,
};

export function useUserProviders() {
  return useQuery({
    queryKey: userProviderKeys.all,
    queryFn: () => userProvidersService.list(),
  });
}

/**
 * invalidateProviderData invalidates both userProviderKeys.all and
 * providerKeys.all: the latter because GET /api/providers changes whenever
 * the user's provider settings change.
 */
function invalidateProviderData(queryClient: ReturnType<typeof useQueryClient>): void {
  void queryClient.invalidateQueries({ queryKey: userProviderKeys.all });
  void queryClient.invalidateQueries({ queryKey: providerKeys.all });
}

export function useCreateUserProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: UserProviderCreate) => userProvidersService.create(input),
    onSuccess: () => invalidateProviderData(queryClient),
  });
}

export function useUpdateUserProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: UserProviderPatch }) =>
      userProvidersService.update(id, patch),
    onSuccess: () => invalidateProviderData(queryClient),
  });
}

export function useDeleteUserProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => userProvidersService.remove(id),
    onSuccess: () => invalidateProviderData(queryClient),
  });
}
