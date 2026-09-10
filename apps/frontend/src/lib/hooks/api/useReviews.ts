import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { reviewsService } from "@/services/api";
import { isTerminal } from "@/types/models/review";
import type { CreateReviewRequest, Review } from "@/types/models/review";

export const reviewKeys = {
  all: ["reviews"] as const,
  lists: () => [...reviewKeys.all, "list"] as const,
  details: () => [...reviewKeys.all, "detail"] as const,
  detail: (id: string) => [...reviewKeys.details(), id] as const,
  files: (id: string) => [...reviewKeys.detail(id), "files"] as const,
  file: (id: string, path: string) => [...reviewKeys.files(id), path] as const,
};

/** useReview polls every 2 s while the review is still building, and stops once terminal. */
export function useReview(id: string | undefined) {
  return useQuery({
    queryKey: reviewKeys.detail(id ?? ""),
    queryFn: () => reviewsService.get(id as string),
    enabled: Boolean(id),
    staleTime: 0,
    refetchInterval: (query) => {
      const data = query.state.data as Review | undefined;
      return data && !isTerminal(data.attributes.status) ? 2000 : false;
    },
  });
}

/**
 * useReviews polls every 2 s while any listed review is still building, and
 * stops once none is -- an idle root page issues no periodic requests (NFR-1).
 */
export function useReviews() {
  return useQuery({
    queryKey: reviewKeys.lists(),
    queryFn: () => reviewsService.list(),
    refetchInterval: (query) => {
      const data = query.state.data as Review[] | undefined;
      return data?.some((review) => !isTerminal(review.attributes.status)) ? 2000 : false;
    },
  });
}

export function useReviewFiles(id: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: reviewKeys.files(id ?? ""),
    queryFn: () => reviewsService.files(id as string),
    enabled: enabled && Boolean(id),
    staleTime: 5 * 60_000,
  });
}

export function useReviewFile(id: string | undefined, path: string | undefined) {
  return useQuery({
    queryKey: reviewKeys.file(id ?? "", path ?? ""),
    queryFn: () => reviewsService.fileDiff(id as string, path as string),
    enabled: Boolean(id) && Boolean(path),
    staleTime: 5 * 60_000,
  });
}

export function useCreateReview() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: CreateReviewRequest) => reviewsService.create(request),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: reviewKeys.lists() });
    },
  });
}

export function useFinishReview() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => reviewsService.remove(id),
    onSettled: (_data, _error, id) => {
      void queryClient.invalidateQueries({ queryKey: reviewKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: reviewKeys.lists() });
    },
  });
}

export function useInvalidateReviews() {
  const queryClient = useQueryClient();
  return {
    invalidateAll: () => queryClient.invalidateQueries({ queryKey: reviewKeys.all }),
    invalidateReview: (id: string) =>
      queryClient.invalidateQueries({ queryKey: reviewKeys.detail(id) }),
  };
}
