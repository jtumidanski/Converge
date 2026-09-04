
---
title: Architecture Overview
description: Core architectural principles of Converge Golang services.
---

# Architecture Overview

Services follow a strict layered design with functional composition and immutability.

## Layers
1. **Domain Layer** — Core logic, models, and validation.
2. **Infrastructure Layer** — Database and external systems.
3. **Transport Layer** — REST endpoints.
4. **Application Layer** — Orchestration and configuration.

## Core Technologies
- Go 1.24+
- GORM ORM (PostgreSQL)
- JSON:API standard
- Logrus structured logging
- OpenTelemetry tracing
- Functional programming with curried functions

## Principles

- **Immutability:** Domain models never mutate.
- **Separation of Concerns:** Domain logic isolated from persistence and transport.
- **Functional Composition:** Use `Provider`, `Map`, and `ParallelMap` for chaining.
- **Stateless Services:** All state in database, services are stateless.

## Sub-Domain / Action-Event Packages

Some services have lightweight sub-domain packages that record action events (e.g., `bucketarchive`, `policyrevoke`, `replicationtrigger`). These packages **still must follow layer separation**:

- If the action is simple (single entity write), **fold it into the parent domain's processor** as a method rather than creating a separate package that violates layers.
- If a separate package is warranted, it must have its own `processor.go` and `administrator.go`. Handlers must not create entities directly or parse JSON manually.
- Sub-domain POST endpoints must use `server.RegisterInputHandler[T]`, not `server.RegisterHandler`.

**Guideline:** Prefer fewer, well-structured packages over many thin packages that cut corners on layer separation.

## Cross-Domain Orchestration

When a handler needs to coordinate across multiple domains (e.g., creating a bucket also creates a default policy):

- **Move the orchestration to the processor layer.** The bucket processor should call the policy processor — not the handler.
- The only exception is read-only aggregation handlers (e.g., dashboard summaries) which may call multiple processors directly. See [anti-patterns.md](anti-patterns.md) for the documented exception.

## Startup Example

```go

db := database.Connect(logger, database.SetMigrations(domain.Migration))
server.New(logger).
  AddRouteInitializer(domain.InitializeRoutes(db)(GetServer())).
  Run()
```
