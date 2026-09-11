import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useCurrentUser } from "@/lib/hooks/api/useAuth";

/**
 * RequireAuth gates a hosted route. An unauthenticated visitor is redirected
 * to /login with the attempted path preserved, so a successful login returns
 * there (FR-8.2, FR-8.3).
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { data, isPending, isError } = useCurrentUser();
  const location = useLocation();
  if (isPending) {
    return <div className="p-10" aria-busy="true" />;
  }
  if (isError || !data) {
    const next = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?next=${next}`} replace />;
  }
  return <>{children}</>;
}
