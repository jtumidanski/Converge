import type { ReactNode } from "react";
import { Link } from "react-router";
import { ThemeToggle } from "@/components/theme/ThemeToggle";

/**
 * AppShell is the persistent application chrome: a wordmark and the theme
 * control, above the routed page.
 *
 * It owns no title or description — pages keep rendering their own PageHeader —
 * and it sets no max width, so each page's own container still governs layout.
 * The header is sticky rather than fixed so it never overlays page content.
 *
 * The optional `right` slot renders beside the theme control — the hosted
 * account menu, when there is one — so the shell itself gains no knowledge
 * of auth.
 */
export function AppShell({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-40 flex h-14 items-center justify-between border-b border-border bg-background px-4">
        <Link to="/" className="text-sm font-semibold text-foreground">
          Converge
        </Link>
        <div className="flex items-center gap-2">
          {right}
          <ThemeToggle />
        </div>
      </header>
      <main className="flex-1">{children}</main>
    </div>
  );
}
