import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
import { useNavigate, useLocation } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { setUnauthorizedHandler } from "@/lib/api/client";

/**
 * AuthProvider registers the 401 handler and nothing else.
 *
 * There is no session state to hold: the cookie is HttpOnly and invisible to
 * JS, so the React Query entry for useCurrentUser *is* the auth state. A 401
 * clears it.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();

  // A ref so the effect below does not re-register the handler on every
  // navigation, which would be a new closure per route change.
  const locationRef = useRef(location);
  useEffect(() => {
    locationRef.current = location;
  }, [location]);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      const path = locationRef.current.pathname;
      // Already on an unauthenticated screen: the 401 from /api/auth/me is
      // the expected answer there, and redirecting would loop.
      if (path === "/login" || path === "/register") {
        return;
      }
      queryClient.clear();
      const next = encodeURIComponent(path + locationRef.current.search);
      void navigate(`/login?next=${next}`, { replace: true });
    });
    return () => setUnauthorizedHandler(null);
  }, [navigate, queryClient]);

  return <>{children}</>;
}
