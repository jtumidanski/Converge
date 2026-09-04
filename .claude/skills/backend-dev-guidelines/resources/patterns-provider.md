
---
title: Provider Pattern
description: Functional data access pattern used for lazy evaluation and error propagation.
---

# Provider Pattern


Encapsulates deferred operations and functional composition for data retrieval.

## Concepts
- Return `model.Provider[T]` for lazy evaluation.
- Compose via `model.Map`, `model.SliceMap`, and `model.ParallelMap`.
- Use `model.ErrorProvider[T]` for error propagation.

## Example
```go
func getById(id uint32) database.EntityProvider[Entity] {
    return func(db *gorm.DB) model.Provider[Entity] {
        var result Entity
        err := db.Where("id = ?", id).First(&result).Error
        if err != nil {
            return model.ErrorProvider[Entity](err)
        }
        return model.FixedProvider(result)
    }
}

// Called from processor with contextualized db:
func (p *ProcessorImpl) ByIdProvider(id uint32) model.Provider[Model] {
    return model.Map(Make)(getById(id)(p.db.WithContext(p.ctx)))
}
```

## Benefits
- Declarative data pipelines
- Clear error handling
- Testable and composable
