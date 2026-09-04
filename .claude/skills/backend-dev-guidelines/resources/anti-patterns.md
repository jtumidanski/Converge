
---
title: Anti-Patterns
description: Common pitfalls to avoid when implementing Golang microservices.
---

# Anti-Patterns


| Anti-Pattern | Why It's Wrong |
|---------------|----------------|
| Business logic in handlers | Breaks separation of concerns |
| **Handlers calling provider functions directly** | **Breaks layer separation - handlers must call processors, not providers** |
| **Direct entity creation in handlers** (`db.Create(&e)` in resource.go) | **Bypasses both processor and administrator layers — all writes must go through administrator functions called by processors** |
| **Cross-domain business logic in handlers** (e.g., handler creating records in another domain) | **Move cross-domain orchestration to the processor layer** |
| Mutable public fields | Violates immutability |
| Database logic in processors | Violates functional purity |
| Missing validation | Allows invalid domain states |
| **`logrus.StandardLogger()` in handlers** | **Use `d.Logger()` from `HandlerDependency` — it carries trace context** |
| **`*logrus.Logger` in processor constructors** | **Use `logrus.FieldLogger` interface — enables `d.Logger()` compatibility and testability** |
| **`server.RegisterHandler` (GET signature) for POST/PATCH endpoints** | **Use `server.RegisterInputHandler[T]` — GET handlers have no request body, forcing manual `io.ReadAll`/`json.Unmarshal`** |
| **Discarding Transform errors with `_`** (e.g., `rm, _ := Transform(m)`) | **Always check and log Transform errors — silent failures mask data conversion bugs** |
| **`os.Getenv()` in handlers** | **Read env vars once at startup via config struct, inject through constructors — per-request `os.Getenv` is wasteful and hard to test** |
| **Eager provider execution** (query immediately, wrap in `FixedProvider`) | **Use `database.Query`/`database.SliceQuery` for lazy (deferred) evaluation — enables composition with `model.Map` and `model.ParallelMap`** |
| Global context usage | Breaks request isolation |
| Manual JSON:API envelope handling | Breaks JSON:API integration, adds boilerplate |
| Nested Data/Type/Attributes in requests | Use flat structures, let api2go handle envelope |
| Custom error response helpers | Just write status codes directly |
| jsonapi struct tags on REST models | Use interface methods (`GetName`, `GetID`, `SetID`) |
| Plain http.HandlerFunc for routes | Use `server.RegisterHandler` for automatic tracing |
| Type aliases for library migrations | Adds indirection; we control all services — update call sites directly |
| Leaving dead code after refactoring | Unused constants/structs/functions clutter the codebase and cause confusion |

**Always** prefer pure, context-aware, curried, and testable functions.

**For REST:** Use `server.RegisterHandler` and `server.RegisterInputHandler` with flat JSON:API-compliant models.

---

## Handler Logger Anti-Pattern

### ❌ Using `logrus.StandardLogger()` in Handlers

**WRONG:**
```go
// resource.go - ANTI-PATTERN
func handleCreateItem(db *gorm.DB) server.InputHandler[CreateRequest] {
    return func(d *server.HandlerDependency, c *server.HandlerContext, req CreateRequest) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            // ❌ WRONG - loses trace context and structured fields
            p := NewProcessor(logrus.StandardLogger(), r.Context(), db)
        }
    }
}
```

**✅ CORRECT:**
```go
// resource.go - CORRECT
func handleCreateItem(db *gorm.DB) server.InputHandler[CreateRequest] {
    return func(d *server.HandlerDependency, c *server.HandlerContext, req CreateRequest) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            // ✅ CORRECT - d.Logger() carries trace ID and handler name
            p := NewProcessor(d.Logger(), r.Context(), db)
        }
    }
}
```

This requires processors to accept `logrus.FieldLogger` (not `*logrus.Logger`):
```go
// processor.go - CORRECT
type Processor struct {
    l   logrus.FieldLogger  // ✅ interface, not concrete type
    ctx context.Context
    db  *gorm.DB
}

func NewProcessor(l logrus.FieldLogger, ctx context.Context, db *gorm.DB) *Processor {
    return &Processor{l: l, ctx: ctx, db: db}
}
```

---

## Wrong Handler Type for POST/PATCH Endpoints

### ❌ Using `RegisterHandler` (GET) for Write Operations

**WRONG:**
```go
// resource.go - ANTI-PATTERN: forces manual body parsing
router.HandleFunc("/items", server.RegisterHandler(l)(si)("create-item", createHandler(db))).Methods(http.MethodPost)

func createHandler(db *gorm.DB) server.GetHandler {  // ❌ GetHandler has no request body
    return func(d *server.HandlerDependency, c *server.HandlerContext) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            body, _ := io.ReadAll(r.Body)           // ❌ manual body reading
            var req CreateRequest
            json.Unmarshal(body, &req)               // ❌ manual JSON parsing
        }
    }
}
```

**✅ CORRECT:**
```go
// resource.go - CORRECT: automatic deserialization
router.HandleFunc("/items", server.RegisterInputHandler[CreateRequest](l)(si)("create-item", createHandler(db))).Methods(http.MethodPost)

func createHandler(db *gorm.DB) server.InputHandler[CreateRequest] {  // ✅ typed request
    return func(d *server.HandlerDependency, c *server.HandlerContext, req CreateRequest) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            // req is already deserialized — use it directly
        }
    }
}
```

---

## Transform Error Handling

### ❌ Discarding Transform Errors

**WRONG:**
```go
// resource.go - ANTI-PATTERN
rm, _ := Transform(m)  // ❌ error silently discarded
server.MarshalResponse[RestModel](d.Logger())(w)(c.ServerInformation())(map[string][]string{})(rm)
```

**✅ CORRECT:**
```go
// resource.go - CORRECT
rm, err := Transform(m)
if err != nil {
    d.Logger().WithError(err).Error("Creating REST model.")
    w.WriteHeader(http.StatusInternalServerError)
    return
}
server.MarshalResponse[RestModel](d.Logger())(w)(c.ServerInformation())(map[string][]string{})(rm)
```

---

## Sub-Domain / Action-Event Packages

Even lightweight packages (e.g., `bucketarchive`, `policyrevoke`, `replicationtrigger`) that record action events **must follow layer separation**:

- **Must have** a `processor.go` (or use the parent domain's processor) for business logic
- **Must have** an `administrator.go` for write operations
- **Must use** `server.RegisterInputHandler[T]` for POST endpoints
- **Must NOT** create entities directly in handlers or parse JSON manually

If the sub-domain is simple enough that a standalone processor adds no value, fold the action into the parent domain's processor as a method instead of creating a separate package with layer violations.

---

## Critical Layer Violations

### ❌ Handlers Calling Providers Directly

**WRONG - Handler bypassing processor:**
```go
// resource.go - ANTI-PATTERN
func handleGetBucketRequest(db *gorm.DB) func(...) http.HandlerFunc {
    return func(d *rest.HandlerDependency, c *rest.HandlerContext) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            // ❌ WRONG - calling provider function directly from handler
            b, err := GetByName(d.Logger(), db)(bucketName)
            // ...
        }
    }
}
```

**Correct layer flow:**
```
resource.go (handler) → processor.go (business logic) → provider.go (data access) → database
```

**✅ CORRECT - Handler calling processor:**
```go
// resource.go - CORRECT PATTERN
func handleGetBucketRequest(db *gorm.DB) func(...) http.HandlerFunc {
    return func(d *rest.HandlerDependency, c *rest.HandlerContext) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            // ✅ CORRECT - calling processor method
            b, err := NewProcessor(d.Logger(), d.Context(), db).GetBucket(bucketName)
            // ...
        }
    }
}
```

**Why this matters:**
1. **Separation of concerns** - Handlers parse requests and marshal responses, processors contain business logic
2. **Testability** - Business logic in processors can be tested without HTTP infrastructure
3. **Reusability** - Processor methods can be called from handlers or other processors
4. **Maintainability** - Changes to data access don't affect handlers
5. **Single responsibility** - Each layer has a clear, focused purpose

**Valid dependencies:**
- ✅ `resource.go` → `processor.go`
- ✅ `processor.go` → `provider.go`
- ✅ `provider.go` → `entity.go` + GORM

**Invalid dependencies:**
- ❌ `resource.go` → `provider.go` (bypasses processor layer)
- ❌ `resource.go` → `entity.go` (bypasses both processor and provider)
- ❌ `processor.go` → `entity.go` directly for database queries (should use provider)

### Exception: Cross-Domain Read-Only Views with Circular Dependencies

In rare cases where circular package dependencies prevent proper layering (e.g., `bucket` imports `policy`, `policy` needs `bucket`), read-only view handlers MAY use providers directly or raw DB queries for cross-domain orchestration.

**When this exception applies:**
- Handler aggregates data from multiple domains
- Circular package dependency prevents calling processors
- Operation is read-only (no state changes)
- Alternative would require significant architectural refactoring

**Example:**
```go
// policy/resource.go - Read-only view handler
func handleGetPoliciesRequest(db *gorm.DB) func(...) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // ⚠️ EXCEPTION: Raw DB query to avoid circular dependency with bucket package
        // Documented reason: bucket package imports policy, can't import bucket here
        var bucketId uuid.UUID
        db.Table("buckets").Select("id").
            Where("name = ?", bucketName).
            Scan(&bucketId)

        // Then use policy provider
        policies, _ := policy.GetByBucketId(...)(bucketId)
        // ...
    }
}
```

**Requirements for using this exception:**
1. Add a comment explaining WHY the circular dependency exists
2. Keep the raw query minimal (single table, simple where clause)
3. Consider architectural refactoring if this pattern appears frequently
4. Never use this exception for write operations - those MUST go through processors

---
