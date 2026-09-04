
---
title: AI Code Generation Guidance
description: Rules for AI agents generating or editing Golang services.
---

# AI Code Generation Guidance

## Mandatory Implementation Workflow

**CRITICAL:** Before implementing ANY code changes, review the [Standard Implementation Workflow](../SKILL.md#standard-implementation-workflow) in the main skill document.

**Key Requirements:**
- ✅ Update mocks immediately when interfaces change
- ✅ Run `go test ./... -count=1` BEFORE claiming completion
- ✅ Fix all test failures before proceeding
- ✅ Report actual test output, not assumptions
- ❌ NEVER skip test execution
- ❌ NEVER assume tests will pass

## Core Rules
1. Respect file responsibilities (see file-responsibilities.md).
2. Maintain immutability and functional composition.
3. Use curried functions for dependency injection.
4. Use `server.RegisterHandler` and `server.RegisterInputHandler` for REST endpoints.
5. Implement JSON:API interface methods on all REST models and request types.
6. **Always verify referenced types exist before using them** - Never assume a type/constant/operation exists.
7. **Always run builds AND tests after code changes** - Verify ALL affected services compile and pass tests.
8. **Always ask before implementing new features** - Get user approval before adding new functionality.

## Validation Rules

### Before Using Any Type/Constant/Operation:
1. **Always verify it exists** - Read the relevant file to confirm the type/constant is implemented
2. **Check all dependent services** - If using a condition type, verify it's in validation/model.go; if using an operation, verify it's in operation_executor.go
3. **Never assume** - Just because it makes logical sense doesn't mean it's implemented

### Examples:
❌ **BAD** - Using condition without verification:
```go
// Assuming "bucketCount" condition exists without checking
conditions := []validation.Condition{
    {Type: "bucketCount", Operator: ">=", Value: "1"},
}
```

✅ **GOOD** - Verify first, ask if missing:
```
1. Read validation/model.go
2. Search for "bucketCount" in ConditionType constants
3. If not found: "The condition type 'bucketCount' doesn't exist in validation/model.go.
   Should I implement it first, or use a different approach?"
```

❌ **BAD** - Using operation without verification:
```go
// Assuming "enable_versioning" operation exists
operation := Operation{
    Type: "enable_versioning",
    Params: map[string]string{"bucketId": "abc", "enabled": "true"},
}
```

✅ **GOOD** - Verify first, ask if missing:
```
1. Read operation_executor.go
2. Search for case "enable_versioning": in the switch statement
3. If not found: "The operation 'enable_versioning' doesn't exist in operation_executor.go.
   To implement it, I would need to:
   - Add case in operation_executor.go
   - Add the necessary domain model and processor methods

   Should I implement this feature first?"
```

## Testing Rules

### After ANY Code Change:
1. **Always run builds for ALL affected services** - Not just the one you modified
2. **Always run tests** for all modified and dependent services
3. **Report failures immediately** - Never commit/continue with failing builds or tests
4. **Update all mocks** - When interfaces change, update ALL mock implementations
5. **No partial implementations** - A feature isn't done until all services build and test successfully

### Build & Test Workflow:
```bash
# CRITICAL: Always build from workspace root to catch cross-service issues
cd /path/to/workspace/root

# Build all affected packages
go build ./...

# If the build fails:
# 1. Report the failure to the user with error details
# 2. Fix ALL compilation errors (missing methods, type mismatches, etc.)
# 3. Re-run the build
# 4. Only proceed when the build succeeds

# After successful builds, run tests
go test ./...

# If tests fail:
# 1. Report the failure to the user
# 2. Fix the tests or code (usually missing mock methods)
# 3. Re-run tests
# 4. Only proceed when all tests pass
```

### Common Build Failures & Fixes:
| Error | Cause | Solution |
|-------|-------|----------|
| `missing method ChangeFace` | Interface updated but mock not updated | Add method to mock struct and implement it |
| `redeclared in this block` | Duplicate function declarations | Remove old/duplicate version |
| `cannot use X as Y value` | Function signature changed incompletely | Update ALL call sites (use `grep -r`) |

### When to Run Tests:
- ✅ After adding new files (model.go, processor.go, etc.)
- ✅ After modifying existing files
- ✅ After adding new dependencies
- ✅ Before creating a pull request
- ✅ After implementing new features
- ❌ Never skip tests "to save time"

## Implementation Rules

### Before Implementing New Features:
1. **ALWAYS ask the user first** if you identify a missing feature during any task
2. **Explain what's missing** and what would need to be implemented
3. **Provide options** for how to proceed
4. **Wait for explicit approval** before implementing

### Example Dialog:
❌ **WRONG** - Implementing without asking:
```
"I noticed the notification feature doesn't exist. Let me implement it for you..."
[Proceeds to implement without approval]
```

✅ **CORRECT** - Ask first:
```
"I need to add a notification feature for this task update, but there is no
notification system implemented yet.

To implement it, I would need to:
1. Add a notification model and entity
2. Add processor methods for creating notifications
3. Add REST endpoints for retrieving notifications

This would add a new domain.

How would you like to proceed?
1. Implement the notification feature first
2. Skip notifications for now
3. Use a different approach
"
```

## Migration & Refactoring Rules

### No Type Aliases During Migrations
When migrating types/functions to a shared library, update ALL service call sites to import from the new library directly. Never leave type aliases (`type Foo = lib.Foo`), re-exports, or thin wrappers that just delegate. We control the full lifecycle of all services — there is no backwards-compatibility concern.

### Clean Up Dead Code After Extraction
After extracting code to a shared library, review every modified service file for symbols that are no longer referenced: unused constants, structs, functions, imports, and variables. Use `grep` across the service to confirm nothing depends on them, then delete. Do not leave dead code behind.

## Commonly Missed Items Checklist

Before reporting any domain package as complete, verify **every item** below. These are the items most frequently missed across all service audits:

- [ ] **`builder.go` exists** for every domain with a model — with `NewBuilder()`, fluent setters, and `Build()` that validates invariants
- [ ] **`ToEntity()` method** exists on the Model type in `entity.go` (not just `Make(Entity) Model`)
- [ ] **`TransformSlice`** exists in `rest.go` alongside `Transform` — list handlers use it (no inline loops)
- [ ] **Processor accepts `logrus.FieldLogger`** (not `*logrus.Logger`) in its constructor
- [ ] **Handlers pass `d.Logger()`** to processors (not `logrus.StandardLogger()`)
- [ ] **POST/PATCH routes use `RegisterInputHandler[T]`** (not `RegisterHandler` which is for GET/DELETE only)
- [ ] **Transform errors are checked and logged** (never `rm, _ := Transform(m)`)
- [ ] **Providers use lazy evaluation** via `database.Query`/`database.SliceQuery` (not eager execution wrapped in `FixedProvider`)
- [ ] **No `os.Getenv()` in handlers** — env vars read once in config, injected via constructors
- [ ] **No cross-domain business logic in handlers** — orchestration belongs in the processor layer
- [ ] **Sub-domain packages have proper layers** — even simple action-event packages need processor + administrator (or fold into parent domain processor)

---

## Generation Workflow
1. **Validate dependencies** - Verify all types/operations you plan to use exist
2. Create `model.go` - Immutable domain model with accessors
3. Map `entity.go` to DB - GORM entities with migrations, including `Make()` and `ToEntity()`
4. Implement `builder.go` - Fluent API for model construction with `Build()` validation
5. Define processors and providers - Pure business logic, `logrus.FieldLogger` in constructors
6. Add `rest.go` - JSON:API DTOs with `Transform` AND `TransformSlice` functions
7. Add `resource.go` - Route registration and thin handlers using `d.Logger()` and correct handler types
8. Write table-driven tests
9. **Run the Commonly Missed Items Checklist above**
10. **Build the project** - From workspace root, run `go build ./...`
11. **Run all tests** - Verify nothing broke with `go test ./... -count=1`
12. **Report build/test results** - Show pass/fail status to user
13. **Fix ALL issues before proceeding** - No partial implementations allowed

## REST Generation Specifics


### When generating `rest.go`:
- ✅ Implement JSON:API interface on all models (`GetName()`, `GetID()`, `SetID()`)
- ✅ Use flat structure for request models (no nested Data/Type/Attributes)
- ✅ Mark ID field with `json:"-"` tag

- ✅ Use pointer fields for optional attributes with `omitempty`
- ✅ Create `Transform(Model) (RestModel, error)` function

- ✅ Create `TransformSlice([]Model) ([]RestModel, error)` function
- ❌ Never use jsonapi struct tags on fields
- ❌ Never create nested Data/Type/Attributes structures

### When generating `resource.go`:
- ✅ Return `func(db *gorm.DB) server.RouteInitializer` from `InitializeRoutes`
- ✅ Use `server.RegisterHandler(l)(si)("handler-name", handler)` for GET/DELETE
- ✅ Use `server.RegisterInputHandler[T](l)(si)("handler-name", handler)` for POST/PATCH
- ✅ Map domain errors to specific HTTP status codes
- ✅ Use `server.MarshalResponse[T]` for success responses
- ✅ Log errors with `d.Logger().WithError(err)`
- ❌ Never manually decode JSON with nested structures
- ❌ Never create custom error response helpers

## Useful Composition
```go
// Transform entity to model using provider pattern
model.Map(Make)(entityProvider)(model.ParallelMap())


// Transform model to REST representation
res, err := ops.Map(Transform)(ops.FixedProvider(model))()

// Transform slice of models to REST representations
res, err := ops.SliceMap(Transform)(ops.FixedProvider(models))(ops.ParallelMap())()
```


## Common Anti-Patterns to Avoid

### ❌ Manual JSON:API Envelope Handling
```go
// DON'T DO THIS
var req struct {
    Data struct {
        Type       string `json:"type"`
        Attributes struct { ... } `json:"attributes"`
    } `json:"data"`
}
json.NewDecoder(r.Body).Decode(&req)
```

### ✅ Use Flat Request Models
```go
// DO THIS
type CreateRequest struct {
    Id   uuid.UUID `json:"-"`
    Name string    `json:"name"`
}
// Let server.RegisterInputHandler handle JSON:API unmarshaling
```

### ❌ Manual HTTP Handler Registration

```go

// DON'T DO THIS
router.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
    // Manual parsing, error handling, etc.
})
```

### ✅ Use server.RegisterHandler
```go
// DO THIS
router.HandleFunc("/users",
    server.RegisterHandler(l)(si)("get-users", listUsersHandler(db))).Methods(http.MethodGet)
```
