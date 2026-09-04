# React Query & Hooks Patterns

## Overview

All server state is managed through TanStack React Query hooks in `lib/hooks/api/`. Each resource has its own hook file exporting query key factories, query hooks, mutation hooks, invalidation helpers, and prefetch utilities.

## Query Key Factory Pattern

**Every hook file exports a hierarchical key factory using `as const`:**

```typescript
// lib/hooks/api/useBuckets.ts
export const bucketKeys = {
  all: ['buckets'] as const,
  lists: () => [...bucketKeys.all, 'list'] as const,
  list: (options?: QueryOptions) =>
    [...bucketKeys.lists(), options] as const,
  details: () => [...bucketKeys.all, 'detail'] as const,
  detail: (id: string) =>
    [...bucketKeys.details(), id] as const,
};
```

**Extended factories for complex resources:**

```typescript
export const policyKeys = {
  all: ['policies'] as const,
  lists: () => [...policyKeys.all, 'list'] as const,
  list: (options?: QueryOptions) => [...policyKeys.lists(), options] as const,
  details: () => [...policyKeys.all, 'detail'] as const,
  detail: (id: string) => [...policyKeys.details(), id] as const,
  // Specialized branches
  byBucket: (bucketId: string) => [...policyKeys.all, 'byBucket', bucketId] as const,
  search: () => [...policyKeys.all, 'search'] as const,
  validation: () => [...policyKeys.all, 'validation'] as const,
};
```

**Key principles:**
- Always use `as const` for immutable tuple types
- Build hierarchically using spread

## Query Hook Pattern

```typescript
export function useBuckets(options?: QueryOptions) {
  return useQuery({
    queryKey: bucketKeys.list(options),
    queryFn: () => bucketsService.getAll(options),
    staleTime: 2 * 60 * 1000,   // ← Per-resource stale time
    gcTime: 5 * 60 * 1000,
  });
}

export function useBucketById(id: string) {
  return useQuery({
    queryKey: bucketKeys.detail(id),
    queryFn: () => bucketsService.getById(id),
    enabled: !!id,
  });
}
```

## Mutation Hook Pattern

```typescript
export function useCreateBucket() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateBucketRequest) =>
      bucketsService.create(data),
    onSettled: () => {
      // Invalidate list to refetch
      queryClient.invalidateQueries({ queryKey: bucketKeys.lists() });
    },
  });
}
```

### Optimistic Update Pattern

```typescript
export function useDeleteObject() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ bucketId, objectId }: { bucketId: string; objectId: string }) =>
      objectsService.deleteObject(bucketId, objectId),

    onMutate: async ({ bucketId, objectId }) => {
      // Cancel in-flight queries
      await queryClient.cancelQueries({
        queryKey: objectKeys.list(bucketId),
      });

      // Snapshot previous value
      const previous = queryClient.getQueryData(
        objectKeys.list(bucketId)
      );

      // Optimistically remove object
      if (previous) {
        queryClient.setQueryData(objectKeys.list(bucketId), {
          ...previous,
          data: previous.data.filter(item => item.id !== objectId),
        });
      }

      return { previous };
    },

    onError: (error, variables, context) => {
      // Rollback on error
      if (context?.previous) {
        queryClient.setQueryData(
          objectKeys.list(variables.bucketId),
          context.previous,
        );
      }
    },

    onSettled: (data, error, { bucketId }) => {
      queryClient.invalidateQueries({
        queryKey: objectKeys.list(bucketId),
      });
    },
  });
}
```

## Invalidation Helper Pattern

**Every hook file exports invalidation utilities:**

```typescript
export function useInvalidateBuckets() {
  const queryClient = useQueryClient();

  return {
    invalidateAll: () =>
      queryClient.invalidateQueries({ queryKey: bucketKeys.all }),
    invalidateLists: () =>
      queryClient.invalidateQueries({ queryKey: bucketKeys.lists() }),
    invalidateBucket: (id: string) =>
      queryClient.invalidateQueries({ queryKey: bucketKeys.detail(id) }),
    clearCache: () => {
      bucketsService.clearServiceCache();
      queryClient.invalidateQueries({ queryKey: bucketKeys.all });
    },
  };
}
```

## Prefetch Pattern

```typescript
export function usePrefetchPolicies() {
  const queryClient = useQueryClient();

  return {
    prefetch: (options?: QueryOptions) =>
      queryClient.prefetchQuery({
        queryKey: policyKeys.list(options),
        queryFn: () => policiesService.getAll(options),
        staleTime: 3 * 60 * 1000,
      }),
  };
}
```

## Stale Time Guidelines

| Data Volatility | Stale Time | GC Time | Examples |
|----------------|-----------|---------|----------|
| High frequency | 30s–1min | 2min | Object listings, recent activity |
| Medium frequency | 1–2min | 5min | Bucket stats, search results |
| Low frequency | 3–5min | 10min | Policies, users, configuration |
| Static data | 5–10min | 15min | Regions, capability flags |
| Validation/existence | 2–5min | 5min | Existence checks, state consistency |

## Hook File Structure

Each hook file follows this order:
1. Key factory export
2. Query hooks (read operations)
3. Specialized query hooks (filtered, by-relation)
4. Mutation hooks (create, update, delete)
5. Batch operation hooks
6. Invalidation helper hook
7. Prefetch helper hook
8. Cache stats hook (if applicable)
