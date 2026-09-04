
---
title: Functional and Builder Patterns
description: Immutability, builders, and curried function patterns for services.
---

# Functional and Builder Patterns


## Immutability
- Domain models have private fields.
- Public getters expose read-only state.
- All mutations occur via builders returning new instances.


## Builder Pattern
```go
m, err := NewBuilder().
  SetId(1).
  SetName("Example").
  Build()
```
- Validation occurs in `Build()`.
- Builders are fluent and chainable.
- `Model.Builder()` supports modification flows.


## Processor Constructor Pattern

All processors must accept `logrus.FieldLogger` (interface), **not** `*logrus.Logger` (concrete type):

```go
type Processor struct {
    l   logrus.FieldLogger
    ctx context.Context
    db  *gorm.DB
}

func NewProcessor(l logrus.FieldLogger, ctx context.Context, db *gorm.DB) *Processor {
    return &Processor{l: l, ctx: ctx, db: db}
}
```

This ensures handlers can pass `d.Logger()` (which returns `logrus.FieldLogger` with trace context) directly to processors.

## Curried Function Pattern
```go
func Create(db *gorm.DB, log logrus.FieldLogger) func(input CreateParams) model.Provider[Entity]
```
- Encourages composition and DI without interfaces.
- Consistent function-first design over interface abstractions.

## Functional Composition
```go
result, err := model.
  Map(Transform)(entityProvider).

  (model.ParallelMap())()
```
