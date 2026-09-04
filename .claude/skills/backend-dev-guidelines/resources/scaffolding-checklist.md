# Service Scaffolding Checklist

When scaffolding a new Converge service, complete ALL of these steps. Do not skip any.

## 1. Service Directory
**Directory:** `services/<service-name>/`

Required structure:
```
services/<service-name>/
├── go.mod
├── cmd/
│   └── main.go
├── internal/
│   └── <domain>/
│       ├── model.go
│       ├── entity.go
│       ├── builder.go
│       ├── processor.go
│       ├── provider.go
│       ├── administrator.go
│       ├── resource.go
│       └── rest.go
├── migrations/
└── Dockerfile
```

Add the module to `go.work` at the repo root.

## 2. Kubernetes Manifest
**File:** `deploy/k8s/<service-name>.yaml`

Two resources: Deployment + Service. Pattern:
```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: <service-name>
spec:
  replicas: 1
  selector:
    matchLabels:
      app: <service-name>
  template:
    metadata:
      labels:
        app: <service-name>
    spec:
      containers:
      - name: <service-name>
        image: ghcr.io/<owner>/converge-<service-name>:latest
        ports:
        - containerPort: 8080
        env:
        - name: DB_HOST
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: DB_HOST
        - name: DB_USER
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: DB_USER
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: DB_PASSWORD
---
apiVersion: v1
kind: Service
metadata:
  name: <service-name>
spec:
  selector:
    app: <service-name>
  ports:
  - protocol: TCP
    port: 8080
```

## 3. Dockerfile
**File:** `services/<service-name>/Dockerfile`

Multi-stage Go build. Key points:
- Builder: `golang:1.24-alpine`
- Runtime: `alpine`
- Copy shared modules first (dependency caching), then service code
- Output binary: `/server`, expose 8080

## 4. Docker Compose Entry
**File:** `deploy/compose/docker-compose.yml`

Add service entry with:
- Build context pointing to service directory
- Port mapping
- Environment variables from `.env`
- Depends on database

## 5. Nginx Routing
**File:** `deploy/compose/nginx.conf` (or equivalent)

Add location block for the service's API path:
```nginx
location /api/v1/<resource-path> {
  proxy_pass http://<service-name>:8080;
}
```

## 6. Bruno Collection
**Directory:** `bruno/<service-name>/`

Minimum files:
```
bruno/<service-name>/
├── bruno.json
└── environments/
    └── Local.bru
```

## 7. CI Configuration
Ensure the service is included in:
- `.github/workflows/` build and test steps
- `scripts/build-<service-name>.sh` script

## 8. Post-Scaffold Verification
After scaffolding is complete, run these skills to verify the work:
1. `/service-doc` — generates/verifies service documentation
2. `/backend-audit` — audits against Converge backend developer guidelines

## 9. Compliance Checklist (Commonly Missed Items)

Before marking scaffolding complete, verify each domain package against these items that are frequently missed during initial implementation:

| Check | File | Requirement |
|-------|------|-------------|
| `builder.go` exists | `builder.go` | Every domain with a model must have a fluent builder with `Build()` validation |
| `ToEntity()` method | `entity.go` | Model must have `ToEntity() Entity` method (not just `Make(Entity) Model`) |
| `TransformSlice` function | `rest.go` | Must exist alongside `Transform` — list handlers must not inline loops |
| `logrus.FieldLogger` | `processor.go` | Constructor must accept `logrus.FieldLogger`, not `*logrus.Logger` |
| `d.Logger()` in handlers | `resource.go` | Handlers must pass `d.Logger()` to processors, not `logrus.StandardLogger()` |
| `RegisterInputHandler[T]` | `resource.go` | POST/PATCH routes must use `RegisterInputHandler[T]`, not `RegisterHandler` |
| Transform error handling | `resource.go` | Never discard Transform errors with `_` — check and log them |
| Lazy providers | `provider.go` | Use `database.Query`/`database.SliceQuery`, not eager execution with `FixedProvider` |
| No `os.Getenv()` in handlers | `resource.go` | Read env vars in config at startup, inject via constructors |

## Database Notes
- Each service owns its own database schema
- All tables use UUID primary keys generated in the application
- Migrations run on service startup
