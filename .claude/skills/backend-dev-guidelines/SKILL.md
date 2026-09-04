---
name: golang-microservice
description: Skill for creating and modifying Golang microservices using DDD, immutable models, functional composition, GORM entities, and JSON:API transport.
---


# Golang Microservice Skill

## Purpose
Provide a composable entry point that activates when working on any Golang service. This skill aligns development and AI generation with architecture patterns and conventions.

## When to Use
Activate when working on:
- Any Go microservice
- Files: `model.go`, `entity.go`, `builder.go`, `processor.go`, `provider.go`, `resource.go`, `rest.go`
- REST JSON:API endpoints
- Testing domain logic, providers, or emission paths

---

## Quick Start Checklist
- [ ] Immutable **Model** with accessors
- [ ] **Entity** with GORM tags and migrations
- [ ] Fluent **Builder** enforcing invariants
- [ ] Pure **Processor** functions
- [ ] Lazy **Provider** for data access
- [ ] **Resource** file for route registration and handlers
- [ ] **Service README** updated if API contracts changed
- [ ] Table-driven **tests** for all logic layers


---

## Standard Implementation Workflow

**MANDATORY:** Follow this workflow for ALL code changes to ensure quality and prevent regressions.

### Implementation Steps

When modifying any service code:

1. **Implement changes** to primary files (model.go, processor.go, etc.)
2. **Update mocks immediately** if any interfaces changed
   - Add corresponding function fields to mock struct
   - Implement new methods with nil-check and default behavior
   - See [Testing Conventions](resources/testing-guide.md#interface-change-workflow) for details
3. **Verify each domain against the Commonly Missed Items Checklist** in [ai-guidance.md](resources/ai-guidance.md#commonly-missed-items-checklist) before moving to the next domain. Do not batch — check domain 1 fully, then domain 2, etc.
4. **Run tests BEFORE claiming completion**:
   ```bash
   go test ./... -count=1
   ```
5. **Fix any failures** - Do NOT skip or ignore test failures
6. **Verify build**:
   ```bash
   go build ./...
   ```
7. **Report test results** with actual command output, not assumptions

### Critical Rules

- **Never skip test execution** - Running tests is mandatory, not optional
- **Never assume tests will pass** - Always verify with actual execution
- **Never update interface without updating mocks** - Causes immediate test failures
- **Always run full test suite** (`go test ./...`) not just modified packages
- **Always use `-count=1` flag** to disable test caching
- **Always verify test output** before marking work complete

### When Tests Fail

If `go test ./... -count=1` reports failures:

1. **Read the error message completely** - Understand what broke
2. **Check for missing mock methods** - Most common cause of failures
3. **Update mocks to match interface** - Add/modify methods as needed
4. **Re-run tests** - Verify the fix didn't break other tests
5. **Do not proceed** until all tests pass

See [Testing Conventions](resources/testing-guide.md) for comprehensive testing guidelines.

---

## Key Principles
1. **Immutability** — Models never mutate; all state changes yield new instances.
2. **Functional Composition** — Use curried functions and providers for composition.
3. **Context Propagation** — Request context (trace, logger, cancellation) is always passed explicitly, not stashed in globals.
4. **Layer Separation** — Each file type has a clear single responsibility.
5. **Pure Logic First** — Business logic runs without side effects.


---

## File Responsibilities

| File | Primary Responsibility | Key Dependencies |
|------|-------------------------|------------------|
| `model.go` | Domain model definition | None |
| `entity.go` | Database schema and migrations | GORM |
| `builder.go` | Fluent construction of valid models | Model |
| `processor.go` | Core business logic | Model, Provider |
| `provider.go` | Lazy database access | GORM, Entity |
| `resource.go` | Route registration and handlers | REST, Processor |
| `rest.go` | JSON:API resource mappings | Model |

---


## Navigation Guide

| Topic | Reference |
|-------|------------|
| Architecture Overview | [resources/architecture-overview.md](resources/architecture-overview.md) |
| File Responsibilities | [resources/file-responsibilities.md](resources/file-responsibilities.md) |
| Functional & Builder Patterns | [resources/patterns-functional.md](resources/patterns-functional.md) |
| Provider Pattern | [resources/patterns-provider.md](resources/patterns-provider.md) |
| REST JSON:API | [resources/patterns-rest-jsonapi.md](resources/patterns-rest-jsonapi.md) |
| Testing Conventions | [resources/testing-guide.md](resources/testing-guide.md) |
| **Service Scaffolding** | **[resources/scaffolding-checklist.md](resources/scaffolding-checklist.md)** |
| AI Code Guidance | [resources/ai-guidance.md](resources/ai-guidance.md) |
| Anti-Patterns | [resources/anti-patterns.md](resources/anti-patterns.md) |

---
