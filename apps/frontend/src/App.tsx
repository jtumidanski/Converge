import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { Toaster } from "sonner";
import { AppRoutes } from "@/routes";
import { AppShell } from "@/components/layout/AppShell";
import { AccountMenu } from "@/components/layout/AccountMenu";
import { ModeGate } from "@/components/auth/ModeGate";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { createQueryClient } from "@/lib/query-client";
import { useTheme } from "@/lib/theme/useTheme";
import { useAuthMode } from "@/lib/hooks/api/useAuth";

const queryClient = createQueryClient();

/**
 * ThemedToaster keeps sonner in step with the app theme. It reads context here,
 * rather than in App, so a theme change does not re-render App itself. The
 * resolved theme is passed, never the literal "system".
 */
function ThemedToaster() {
  const { resolved } = useTheme();
  return <Toaster richColors position="top-right" theme={resolved} />;
}

/**
 * ModeAwareApp reads the already-resolved mode (ModeGate fetched it, and
 * useAuthMode's staleTime is Infinity, so this is a cache read, not a second
 * request) to decide whether the shell gets an account menu and whether the
 * routes are guarded.
 */
function ModeAwareApp() {
  const { data } = useAuthMode();
  const hosted = data?.mode === "hosted";
  return (
    <AppShell right={hosted ? <AccountMenu /> : undefined}>
      <AppRoutes hosted={hosted} />
    </AppShell>
  );
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <BrowserRouter>
          <ModeGate>
            <ModeAwareApp />
          </ModeGate>
          <ThemedToaster />
        </BrowserRouter>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
