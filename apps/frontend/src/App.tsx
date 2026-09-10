import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { Toaster } from "sonner";
import { AppRoutes } from "@/routes";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { createQueryClient } from "@/lib/query-client";
import { useTheme } from "@/lib/theme/useTheme";

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

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <BrowserRouter>
          <AppShell>
            <AppRoutes />
          </AppShell>
          <ThemedToaster />
        </BrowserRouter>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
