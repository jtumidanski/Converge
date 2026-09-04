# Service Layer Patterns

## Overview

The service layer (`services/api/`) provides typed abstractions over the API client. Services are **singletons** — instantiated once and exported as module-level constants.

## Two Service Patterns

### 1. BaseService Pattern (Preferred for Complex Resources)

Extend `BaseService` for resources that need validation, transformation, or batch operations.

```typescript
// services/api/bans.service.ts
class BansService extends BaseService {
  protected basePath = '/api/bans';

  // Override validation for create/update
  protected override validate<T>(data: T): ValidationError[] {
    const errors: ValidationError[] = [];
    if (this.isCreateBanRequest(data)) {
      if (!data.value || data.value.trim().length === 0) {
        errors.push({ field: 'value', message: 'Ban value is required' });
      }
    }
    return errors;
  }

  // Override response transformation
  protected override transformResponse<T>(data: T): T {
    if (this.isBan(data)) {
      return {
        ...data,
        attributes: {
          ...data.attributes,
          banType: Number(data.attributes.banType),
          permanent: Boolean(data.attributes.permanent),
        },
      } as T;
    }
    return data;
  }

  // Public methods
  async getAllBans(options?: QueryOptions): Promise<Ban[]> {
    const bans = await api.getList<Ban>(this.basePath, options);
    return bans.map(item => this.transformResponse(item));
  }

  // Type guard (private)
  private isBan(data: unknown): data is Ban {
    return typeof data === 'object' && data !== null
      && 'id' in data && 'attributes' in data;
  }
}

export const bansService = new BansService();
```

### 2. Direct API Client Pattern (Simple Resources)

For services without validation or transformation needs.

```typescript
// services/api/buckets.service.ts
class BucketsService {
  private basePath = '/api/buckets';

  async getAll(options?: ServiceOptions): Promise<Bucket[]> {
    return api.getList<Bucket>(this.basePath, options);
  }

  async getById(bucketId: string, options?: ServiceOptions): Promise<Bucket> {
    return api.getOne<Bucket>(`${this.basePath}/${bucketId}`, options);
  }

  async update(bucketId: string, data: UpdateBucketData): Promise<void> {
    await api.patch(`${this.basePath}/${bucketId}`, {
      data: {
        type: "buckets",
        id: bucketId,
        attributes: data,
      },
    });
  }
}

export const bucketsService = new BucketsService();
```

## BaseService Template Methods

| Method | Purpose |
|--------|---------|
| `getAll<T>(options?)` | Fetch list with query support |
| `getById<T>(id, options?)` | Fetch single by ID |
| `exists(id, options?)` | Check existence (handles 404 → false) |
| `create<T, D>(data, options?)` | POST with validation |
| `update<T, D>(id, data, options?)` | PUT with validation |
| `patch<T, D>(id, data, options?)` | PATCH for partial updates |
| `delete(id, options?)` | DELETE resource |
| `createBatch<T, D>(items, options?, batchOptions?)` | Concurrent creates |
| `updateBatch<T, D>(updates, options?, batchOptions?)` | Concurrent updates |
| `deleteBatch(ids, options?, batchOptions?)` | Concurrent deletes |

## JSON:API Request Format

All write operations use JSON:API envelope:

```typescript
{
  data: {
    type: "resourceType",
    id: "identifier",        // Required for update/patch
    attributes: { /* data */ }
  }
}
```

### Action endpoints (no real attributes)

This applies even to "action" endpoints whose body has no attributes — archive, unarchive, sync, lock, unlock, and similar. If the backend route is wired through `server.RegisterInputHandler[T]` (the common case for any non-GET endpoint that has a typed `XxxRequest` in the corresponding `rest.go`), it parses the body as a JSON:API envelope *before* the handler runs. A bare `{}` body returns `400 "Could not parse request body"` and the action never executes.

For these endpoints, send the envelope with empty attributes:

```typescript
return api.post<ApiResponse<Bucket>>(`/buckets/${id}/archive`, {
  data: { type: "buckets", id, attributes: {} },
});
```

Match the `type` to the backend `XxxRequest`'s `GetName()` value (which is sometimes the parent resource type like `"buckets"` and sometimes a dedicated action type like `"replication-triggers"` — check the corresponding service's `rest.go`).

A small number of backend action endpoints use `server.RegisterHandler` instead of `RegisterInputHandler[T]` and ignore the body entirely (e.g. lock/unlock, archive/unarchive). Sending a bare `{}` to those is harmless. When in doubt, default to the JSON:API envelope — the `RegisterHandler`-based endpoints will accept it too.

## Update Pattern (Immutable)

Merge existing attributes with updates, return new object:

```typescript
async updateBucket(bucket: BucketBasic, updates: Partial<BucketBasicAttributes>): Promise<BucketBasic> {
  const input = {
    data: {
      id: bucket.id,
      type: 'buckets',
      attributes: { ...bucket.attributes, ...updates },
    },
  };
  await this.patch<void, typeof input>(bucket.id, input);
  return { ...bucket, attributes: { ...bucket.attributes, ...updates } };
}
```

## Exports (index.ts)

```typescript
// Base
export { BaseService } from './base.service';
export type { ServiceOptions, QueryOptions, BatchResult, ValidationError } from './base.service';

// Singleton instances
export { bansService } from './bans.service';
export { bucketsService } from './buckets.service';
export { policiesService } from './policies.service';
// ...

// Types re-exported per service
export type { Ban, BanAttributes, CreateBanRequest } from './bans.service';
```
