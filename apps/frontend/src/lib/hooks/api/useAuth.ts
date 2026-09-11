import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { authService } from "@/services/api";

export const authKeys = {
  mode: ["auth", "mode"] as const,
  me: ["auth", "me"] as const,
};

export function useAuthMode() {
  return useQuery({
    queryKey: authKeys.mode,
    queryFn: () => authService.mode(),
    // The mode is fixed for the process lifetime (FR-1.6), so this is fetched
    // once per page load and never again.
    staleTime: Infinity,
    retry: false,
  });
}

export function useCurrentUser() {
  return useQuery({
    queryKey: authKeys.me,
    queryFn: () => authService.me(),
    // No retry: a 401 here is the normal "not signed in" answer, and retrying
    // would delay the redirect and multiply the 401s.
    retry: false,
    staleTime: 60_000,
  });
}

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      authService.login(username, password),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: authKeys.me });
    },
  });
}

export function useRegister() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      authService.register(username, password),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: authKeys.me });
    },
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => authService.logout(),
    // Clears every cached entry, not just auth.me, so no data from the
    // previous account survives in the same tab for the next user to sign
    // into on this browser.
    onSuccess: () => {
      queryClient.clear();
    },
  });
}

export function useChangePassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      currentPassword,
      newPassword,
    }: {
      currentPassword: string;
      newPassword: string;
    }) => authService.changePassword(currentPassword, newPassword),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: authKeys.me });
    },
  });
}

export function useDeleteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ password }: { password: string }) => authService.deleteAccount(password),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: authKeys.me });
    },
  });
}
