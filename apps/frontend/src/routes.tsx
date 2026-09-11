import { Route, Routes } from "react-router";
import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";
import { SelectChangesPage } from "@/pages/SelectChangesPage";
import { ReviewPage } from "@/pages/ReviewPage";
import { LoginPage } from "@/pages/LoginPage";
import { RegisterPage } from "@/pages/RegisterPage";
import { ProviderSettingsPage } from "@/pages/ProviderSettingsPage";
import { AccountSettingsPage } from "@/pages/AccountSettingsPage";
import { EmptyState } from "@/components/common/EmptyState";
import { RequireAuth } from "@/components/auth/RequireAuth";

const notFound = (
  <div className="mx-auto max-w-3xl p-10">
    <EmptyState title="Page not found" description="That address does not exist." />
  </div>
);

/**
 * AppRoutes branches on `hosted`. Standalone renders the same three routes as
 * before, unwrapped — no guard component mounts at all, which is what keeps
 * standalone's behaviour identical to a build with no auth feature (FR-8.1).
 * Hosted wraps those same three routes in RequireAuth and adds /login,
 * /register, and the two settings routes.
 */
export function AppRoutes({ hosted }: { hosted: boolean }) {
  if (!hosted) {
    return (
      <Routes>
        <Route path="/" element={<SelectRepositoryPage />} />
        <Route path="/select" element={<SelectChangesPage />} />
        <Route path="/reviews/:id" element={<ReviewPage />} />
        <Route path="*" element={notFound} />
      </Routes>
    );
  }
  return (
    <Routes>
      <Route
        path="/"
        element={
          <RequireAuth>
            <SelectRepositoryPage />
          </RequireAuth>
        }
      />
      <Route
        path="/select"
        element={
          <RequireAuth>
            <SelectChangesPage />
          </RequireAuth>
        }
      />
      <Route
        path="/reviews/:id"
        element={
          <RequireAuth>
            <ReviewPage />
          </RequireAuth>
        }
      />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route
        path="/settings/providers"
        element={
          <RequireAuth>
            <ProviderSettingsPage />
          </RequireAuth>
        }
      />
      <Route
        path="/settings/account"
        element={
          <RequireAuth>
            <AccountSettingsPage />
          </RequireAuth>
        }
      />
      <Route path="*" element={notFound} />
    </Routes>
  );
}
