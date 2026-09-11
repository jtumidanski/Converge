import type { ReactNode } from "react";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { useAuthMode } from "@/lib/hooks/api/useAuth";
import { AuthProvider } from "@/components/auth/AuthProvider";

/**
 * ModeGate branches the whole tree on GET /api/auth/mode.
 *
 * Standalone resolves straight to today's tree: the same AppShell, the same
 * AppRoutes, no auth provider mounted, no extra network calls. Hosted mounts
 * the auth-aware shell. One conditional at the root beats a mode check in
 * every component (design §9).
 */
export function ModeGate({ children }: { children: ReactNode }) {
  const { data, isPending, isError } = useAuthMode();
  if (isPending) {
    return <div className="p-10" aria-busy="true" />;
  }
  if (isError || !data) {
    return (
      <div className="mx-auto max-w-3xl p-10">
        <ErrorBanner title="Converge could not determine how this instance is configured." />
      </div>
    );
  }
  if (data.mode === "standalone") {
    return <>{children}</>;
  }
  return <AuthProvider>{children}</AuthProvider>;
}
