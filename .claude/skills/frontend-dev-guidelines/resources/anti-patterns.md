# Anti-Patterns

## Quick Reference

| Anti-Pattern | Correct Pattern |
|-------------|-----------------|
| Manual class concatenation | Use `cn()` utility |
| `any` type | Proper types or type guards |
| Direct API calls in components | Use service layer → hooks |
| Inline Zod schemas in components | Define in `lib/schemas/` |
| Spinner for page loading | Skeleton components |
| `console.log` for errors | `toast.error()` + `createErrorFromUnknown()` |
| Hardcoded color values | Semantic CSS variables (`bg-background`) |
| State mutation | Spread operator for immutable updates |
| Default export for components | Named export |

---

## Detailed Anti-Patterns

### 1. Calling API Client Directly from Components

```tsx
// ❌ Bad — bypasses service layer
import { api } from "@/lib/api/client";
const data = await api.get('/api/buckets');

// ✅ Good — uses service abstraction
import { bucketsService } from "@/services/api";
const data = await bucketsService.getAll();
```

### 2. Manual Class String Concatenation

```tsx
// ❌ Bad — no merge, duplicates not resolved
<div className={"flex items-center " + (active ? "bg-primary" : "")} />

// ✅ Good — cn() handles merging and deduplication
<div className={cn("flex items-center", active && "bg-primary")} />
```

### 3. Using `any` Type

```typescript
// ❌ Bad — defeats TypeScript
const handleData = (data: any) => { ... };

// ✅ Good — proper typing
const handleData = (data: Bucket) => { ... };

// ✅ Good — unknown + type guard for dynamic data
const handleData = (data: unknown) => {
  if (isBucket(data)) { /* typed access */ }
};
```

### 4. Inline Schema Definition

```tsx
// ❌ Bad — schema buried in component, not reusable
export function CreateDialog() {
  const schema = z.object({ name: z.string().min(1) });
  // ...
}

// ✅ Good — schema in dedicated file
// lib/schemas/resource.schema.ts
export const createResourceSchema = z.object({ name: z.string().min(1) });
export type CreateResourceFormData = z.infer<typeof createResourceSchema>;

// Component imports schema
import { createResourceSchema, type CreateResourceFormData } from "@/lib/schemas/resource.schema";
```

**Exception:** Cross-field `.refine()` validations that are form-specific can live in the component file.

### 5. Spinner for Content Loading

```tsx
// ❌ Bad — jarring, no layout stability
if (loading) return <div className="animate-spin">Loading...</div>;

// ✅ Good — preserves layout during loading
if (loading) return <PageSkeleton />;

// ✅ Good — spinner only for submit buttons
<Button disabled={isSubmitting}>
  {isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
  Submit
</Button>
```

### 6. Hardcoded Colors

```tsx
// ❌ Bad — ignores theme, breaks dark mode
<div className="bg-white text-gray-900 border-gray-200" />

// ✅ Good — uses semantic CSS variables
<div className="bg-background text-foreground border-border" />
<p className="text-muted-foreground" />
```

### 7. Mutating State

```tsx
// ❌ Bad — direct mutation
const updatedBuckets = buckets;
updatedBuckets.push(newBucket);
setBuckets(updatedBuckets);

// ✅ Good — immutable update
setBuckets([...buckets, newBucket]);

// ✅ Good — immutable object update
setBucket({ ...bucket, attributes: { ...bucket.attributes, ...updates } });
```

### 8. Missing Error Handling in Async Operations

```tsx
// ❌ Bad — unhandled rejection
useEffect(() => {
  bucketsService.getAll().then(setBuckets);
}, []);

// ✅ Good — proper error handling with user feedback
useEffect(() => {
  bucketsService.getAll()
    .then(data => { setBuckets(data); setError(null); })
    .catch(err => {
      const errorInfo = createErrorFromUnknown(err, "Failed to fetch buckets");
      setError(errorInfo.message);
    })
    .finally(() => setLoading(false));
}, []);
```

### 9. Default Exports for Components

```tsx
// ❌ Bad — unnamed in imports, harder to refactor
export default function BucketList() { ... }

// ✅ Good — explicit naming, better IDE support
export function BucketList() { ... }
```
