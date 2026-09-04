
---
title: File Responsibilities
description: Responsibilities of each standard file in a domain package.
---

# File Responsibilities

## `model.go`
Defines immutable domain objects with private fields and accessor methods.

## `entity.go`

Database entity definitions and migration helpers using GORM.

**Required functions:**
- `Make(Entity) (Model, error)` — converts a database entity to an immutable domain model
- `ToEntity() Entity` — method on Model that converts back to a database entity

Both directions are mandatory. `Make` is used after reads; `ToEntity()` is used before writes.

## `builder.go`

Fluent API for constructing validated domain models. `Build()` enforces invariants.


## `processor.go`
Business logic orchestration.

**Constructor signature:** `NewProcessor(l logrus.FieldLogger, ctx context.Context, db *gorm.DB)`
- The logger parameter **must** be `logrus.FieldLogger` (interface), **not** `*logrus.Logger` (concrete type). This ensures compatibility with `d.Logger()` from handlers.

**Key Responsibilities:**
- Orchestrate providers (reads) and administrators (writes)
- Enforce business rules and invariants
- Always use `p.db.WithContext(p.ctx)` when calling providers or administrators

**Critical Rules:**
- ✅ `processor.go` → `provider.go` for reads (correct)
- ✅ `processor.go` → `administrator.go` for writes (correct)
- ❌ `processor.go` → direct `db.Create`/`db.Save`/`db.Delete` (WRONG - use administrator functions)

## `administrator.go`

**Write operations** (create, update, delete) that modify database state. This file handles all state-changing database access.

**Key Responsibilities:**
- Define write functions that accept `*gorm.DB` as the first parameter
- Return the modified Entity (not Model) — the processor converts via `Make`

**Typical Signatures:**
```go
func create(db *gorm.DB, name string) (Entity, error)
func update(db *gorm.DB, id uuid.UUID, name string) (Entity, error)
func deleteByID(db *gorm.DB, id uuid.UUID) error
```

**Why This Separation:**
- **Testability** — Read and write operations can be mocked independently
- **Clear intent** — Code review can quickly identify state-changing operations
- **Single responsibility** — Each file has one job

## `provider.go`

**Read operations** (queries) that fetch data without side effects. This file handles all read-only database access.

**Key Responsibilities:**
- Define query functions returning `database.EntityProvider[T]` or `database.EntityProvider[[]T]`
- Provide `modelFromEntity(e entity) (Model, error)` transformation function
- Use `database.Query[T]` for single-entity lookups
- Use `database.SliceQuery[T]` for multi-entity queries
- Never modify database state

**Typical Signatures:**
```go
func getById(id uint32) database.EntityProvider[entity]
func getByName(name string) database.EntityProvider[entity]
func getAll() database.EntityProvider[[]entity]
func modelFromEntity(e entity) (Model, error)
```

**Pattern:** Provider functions are curried - they accept query parameters and return a function that takes `*gorm.DB` and returns `model.Provider[T]`. The `*gorm.DB` should have context set via `db.WithContext(ctx)`. This enables lazy evaluation and composition with `model.Map`, `model.SliceMap`, and `model.ParallelMap`.

**Why This Separation:**
- **Testability** - Read and write operations can be mocked independently
- **Pure composition** - Read path remains side-effect free, enabling functional composition
- **Clear intent** - Code review can quickly identify state-changing operations


## `resource.go`
Route registration and handler definitions for REST endpoints.

**Key Responsibilities:**
- Define `InitializeRoutes(si jsonapi.ServerInformation) func(db *gorm.DB) server.RouteInitializer` for route registration
- Use `server.RegisterHandler(l)(si)` for GET/DELETE handlers (no request body)
- Use `server.RegisterInputHandler[T](l)(si)` for POST/PATCH handlers (with typed request model)
- Implement handler functions matching `server.GetHandler` or `server.InputHandler[T]` signatures
- Map domain errors to HTTP status codes (400, 404, 409, 500)
- **Delegate ALL business logic to processors - NEVER call provider functions directly**
- Use `server.MarshalResponse[T]` for successful responses
- Log errors with context using `d.Logger().WithError(err)`

**Pattern:** Thin handlers that parse input, invoke processors, handle errors, and marshal responses.

**Critical Rules:**
- ✅ `resource.go` → `processor.go` (correct)
- ❌ `resource.go` → `provider.go` (WRONG - bypasses business logic layer)
- ❌ `resource.go` → database/GORM (WRONG - bypasses all layers)


## `rest.go`
Serialization and transformation between domain models and JSON:API.

**Key Responsibilities:**
- Define `RestModel` struct implementing JSON:API interface (`GetName()`, `GetID()`, `SetID()`)
- Define request models (`CreateRequest`, `UpdateRequest`) implementing JSON:API interface
- Implement `Transform(Model) (RestModel, error)` to convert domain models to REST representations
- Implement `TransformSlice([]Model) ([]RestModel, error)` for bulk transformations
- Use flat structure for request models (no nested Data/Type/Attributes)
- Mark ID field with `json:"-"` tag (set via SetID)
- Use pointer fields for optional attributes with `omitempty`

**Both `Transform` and `TransformSlice` are mandatory.** List handlers must use `TransformSlice` — do not inline transform loops in resource.go.

**Pattern:** JSON:API-compliant DTOs with automatic marshaling/unmarshaling via api2go library.



