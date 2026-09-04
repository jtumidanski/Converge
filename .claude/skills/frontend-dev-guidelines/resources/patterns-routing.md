# Routing & Pages Patterns

## Overview

Converge UI uses React Router for client-side routing. All pages are client-rendered — this is a SPA that relies on React hooks and browser APIs.

## Route Structure

```
pages/
├── LoginPage.tsx            # Login page
├── DashboardPage.tsx        # Dashboard home
├── BucketsPage.tsx          # Bucket list
├── PoliciesPage.tsx         # Policy list
├── UsersPage.tsx            # User management
├── SettingsPage.tsx         # Settings
└── NotFoundPage.tsx         # 404 handler
```

## Route Configuration

```tsx
// App.tsx or routes.tsx
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<AppLayout />}>
          <Route path="/" element={<Navigate to="/app" replace />} />
          <Route path="/app" element={<DashboardPage />} />
          <Route path="/app/buckets" element={<BucketsPage />} />
          <Route path="/app/policies" element={<PoliciesPage />} />
          <Route path="/app/users" element={<UsersPage />} />
          <Route path="/app/settings" element={<SettingsPage />} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </BrowserRouter>
  );
}
```

## List Page Pattern

```tsx
import { useState, useEffect, useCallback } from "react";
import { DataTable } from "@/components/data-table";

export function BucketsPage() {
  const [buckets, setBuckets] = useState<Bucket[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchBuckets = useCallback(async () => {
    setLoading(true);
    try {
      const data = await bucketsService.getAll();
      setBuckets(data);
    } catch (err) {
      const errorInfo = createErrorFromUnknown(err, "Failed to fetch buckets");
      toast.error(errorInfo.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchBuckets(); }, [fetchBuckets]);

  if (loading) return <BucketsPageSkeleton />;

  return (
    <div className="flex flex-col gap-4 p-4">
      <DataTable columns={columns} data={buckets} onRefresh={fetchBuckets} />
    </div>
  );
}
```

## Detail Page Pattern

```tsx
import { useParams, useNavigate } from "react-router-dom";

export function BucketDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  // ... fetch and display logic
}
```

## Root Layout

The root layout wraps the app with providers:

```tsx
// App.tsx
export function App() {
  return (
    <QueryProvider>
      <ThemeProvider defaultTheme="dark">
        <BrowserRouter>
          <Routes>
            {/* routes */}
          </Routes>
        </BrowserRouter>
        <Toaster />
      </ThemeProvider>
    </QueryProvider>
  );
}
```

## Navigation Patterns

- **Sidebar:** Static navigation groups defined in `app-sidebar.tsx`
- **Navigation:** `useNavigate()` hook from React Router
- **Back navigation:** `navigate(-1)` or `navigate("/parent-route")`
- **Post-action redirect:** `navigate("/resource/" + id)` after success
