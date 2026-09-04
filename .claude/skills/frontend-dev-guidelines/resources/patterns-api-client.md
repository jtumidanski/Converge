# API Client Patterns

## Overview

The API client (`lib/api/client.ts`) is a singleton `ApiClient` class that provides all HTTP communication. It includes request deduplication, response caching, retry with exponential backoff, and progress tracking.

## Singleton Instance

```typescript
// lib/api/client.ts
const api = {
  getList: <T>(url: string, options?) => Promise<T[]>,
  getOne: <T>(url: string, options?) => Promise<T>,
  get: <T>(url: string, options?) => Promise<T>,
  post: <T>(url: string, data?, options?) => Promise<T>,
  put: <T>(url: string, data?, options?) => Promise<T>,
  patch: <T>(url: string, data?, options?) => Promise<T>,
  delete: <T>(url: string, options?) => Promise<T>,
  upload: <T>(url: string, file, options?) => Promise<T>,
  download: (url: string, options?) => Promise<Blob>,
  clearCache: () => void,
  clearCacheByPattern: (pattern: string) => void,
  getCacheStats: () => CacheStats,
  clearPendingRequests: () => void,
};
```

## Request Options

```typescript
interface ApiRequestOptions {
  timeout?: number;               // Default 30s
  headers?: HeadersInit;          // Additional headers
  maxRetries?: number;            // Default 3
  retryDelay?: number;            // Default 1000ms
  maxRetryDelay?: number;         // Default 10000ms
  exponentialBackoff?: boolean;   // Default true
  signal?: AbortSignal;           // Cancellation
  skipDeduplication?: boolean;    // Disable request dedup
  onProgress?: ProgressCallback;  // Upload/download progress
  cacheConfig?: CacheOptions | false;  // Cache control
}
```

## Caching

The API client has built-in response caching with TTL:

```typescript
interface CacheOptions {
  ttl?: number;                    // Default 5 minutes
  keyPrefix?: string;              // Cache key namespace
  staleWhileRevalidate?: boolean;  // Serve stale while refreshing
  maxStaleTime?: number;           // Default 1 minute
}
```

**Cache helpers:**
```typescript
const cache = {
  defaultOptions: () => CacheOptions,
  shortLived: () => CacheOptions,       // 1 minute
  longLived: () => CacheOptions,        // 15 minutes
  staleWhileRevalidate: (ttl?, maxStaleTime?) => CacheOptions,
  withTTL: (ttlMinutes) => CacheOptions,
  withPrefix: (keyPrefix, ttl?) => CacheOptions,
  disable: () => false,                 // No caching
};
```

**Note:** Most React Query hooks pass `useCache: false` to the service layer since React Query manages its own cache. The API client cache is more useful for non-hook contexts.

## Request Deduplication

GET and POST requests with identical URLs and bodies are automatically deduplicated — only one network request fires, and all callers receive the same promise.

```typescript
// These fire only ONE network request:
Promise.all([
  api.get('/api/buckets'),
  api.get('/api/buckets'),
  api.get('/api/buckets'),
]);
```

## Retry Logic

Failed requests retry with exponential backoff:

- Default: 3 retries
- Delay: 1s → 2s → 4s (exponential, max 10s)
- Only retries on retryable errors (network, 5xx)
- Does not retry 4xx errors (client errors)

## Cancellation

```typescript
const cancellation = {
  createController: () => AbortController,
  createTimeoutController: (timeoutMs) => AbortController,
  combineSignals: (...signals) => AbortController,
  isCancellationError: (error) => boolean,
};
```

## Error Handling

The API client throws typed errors that can be classified:

```typescript
import { isRetryableError, requiresAuthentication, getErrorActions } from "@/lib/api/errors";

try {
  const data = await api.get('/api/resource');
} catch (error) {
  if (requiresAuthentication(error)) {
    // Redirect to login
  } else if (isRetryableError(error)) {
    // Auto-retry or show retry button
  } else {
    const message = transformError(error, { context: 'loading resource' });
    toast.error(message);
  }
}
```

## Progress Tracking (Uploads/Downloads)

```typescript
const progress = {
  createFormData: (files, fieldName?) => FormData,
  getTotalSize: (files) => number,
  formatBytes: (bytes, decimals?) => string,
  formatRate: (bytesPerSecond) => string,
  formatTimeRemaining: (milliseconds) => string,
};

// Usage
await api.upload('/api/upload', file, {
  onProgress: ({ loaded, total, percentage }) => {
    setUploadProgress(percentage);
  },
});
```
