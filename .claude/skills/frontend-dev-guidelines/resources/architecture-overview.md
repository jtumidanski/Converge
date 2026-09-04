# Architecture Overview

## Tech Stack

| Technology | Purpose |
|-----------|---------|
| React | UI framework |
| Vite | Build tool, dev server |
| TypeScript | Type safety (strict mode) |
| React Router | Client-side routing |
| Tailwind CSS | Utility-first styling |
| shadcn/ui | Radix-based component library |
| TanStack React Query | Server state management |
| TanStack React Table | Data table rendering |
| react-hook-form | Form state management |
| Zod | Schema validation |
| sonner | Toast notifications |
| Lucide React | Icon library |

## Project Structure

```
frontend/
├── pages/                  # Route pages
├── components/
│   ├── ui/                 # shadcn/ui base primitives
│   ├── common/             # Shared presentational components
│   ├── features/           # Feature-specific containers
│   └── providers/          # React context provider wrappers
├── lib/
│   ├── api/                # API client + error utilities
│   ├── hooks/api/          # React Query hooks (per-resource)
│   ├── schemas/            # Zod validation schemas
│   ├── breadcrumbs/        # Breadcrumb navigation system
│   └── utils.ts            # cn() classname helper
├── services/api/           # Service classes (BaseService + concrete)
├── types/
│   ├── models/             # Domain model interfaces
│   └── api/                # API response/error types
├── context/                # React context definitions
└── tests/                  # Test files
```

## Architectural Layers

```
┌─────────────────────────────────────────────┐
│  Route Pages (pages/*.tsx)                    │  ← Data fetching, composition
├─────────────────────────────────────────────┤
│  Feature Components (components/features/)   │  ← Business UI logic
├─────────────────────────────────────────────┤
│  React Query Hooks (lib/hooks/api/)          │  ← Server state, cache
├─────────────────────────────────────────────┤
│  Service Layer (services/api/)               │  ← API abstraction
├─────────────────────────────────────────────┤
│  API Client (lib/api/client.ts)              │  ← HTTP, retry, dedup
├─────────────────────────────────────────────┤
│  Backend Service                             │  ← Go service via ingress
└─────────────────────────────────────────────┘
```

**Data flows top-down.** Pages compose feature components, which use hooks, which call services, which use the API client. Never skip layers (e.g., components should not call `api.get()` directly).

## Provider Hierarchy (Root Layout)

```tsx
// App.tsx
<BrowserRouter>
  <QueryProvider>
    <ThemeProvider>
      <AuthProvider>
        {children}
      </AuthProvider>
    </ThemeProvider>
  </QueryProvider>
</BrowserRouter>
```

**Order matters (outermost → innermost):**
1. `BrowserRouter` — routing context required by all navigation
2. `QueryProvider` — wraps React Query client for data fetching
3. `ThemeProvider` — dark/light mode toggling
4. `AuthProvider` — authentication state (depends on QueryProvider)

## Key Configuration

### TypeScript (tsconfig.json)
- `strict: true` — all strict checks enabled
- `noUncheckedIndexedAccess: true` — safe index access
- `exactOptionalPropertyTypes: true` — precise optional types
- `noImplicitOverride: true` — explicit override keyword
- Path alias: `@/*` maps to project root

### React Query (lib/query-client.ts)
- Stale time: 5 minutes default
- GC time: 10 minutes default
- Retry: 3 attempts with exponential backoff (max 30s)
- `refetchOnWindowFocus: false`
- Mutations: 1 retry, 1s delay

### Vite (vite.config.ts)
- React plugin enabled
- Path alias: `@/*` maps to project root
- Dev server proxy for API routes
