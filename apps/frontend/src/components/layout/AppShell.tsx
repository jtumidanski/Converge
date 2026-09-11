import { useCallback, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { ThemeToggle } from "@/components/theme/ThemeToggle";
import { BrandMark } from "@/components/layout/BrandMark";
import { Breadcrumbs } from "@/components/layout/Breadcrumbs";
import {
  BreadcrumbSegmentsContext,
  BreadcrumbSetterContext,
  type BreadcrumbSegment,
} from "@/lib/breadcrumbs/context";
import { strings } from "@/lib/strings";

/**
 * AppShell is the persistent chrome: a bordered brand block, the current
 * page's breadcrumb, and the theme control, above one centred container that
 * every route shares. Pages no longer set their own max width -- one width and
 * one horizontal padding for all three routes is the point of the redesign.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const [segments, setSegmentsState] = useState<BreadcrumbSegment[]>([]);
  // Stable across every render: publishing pages subscribe to this via
  // useBreadcrumbs and must never re-render just because the displayed
  // segments changed. See the comment on BreadcrumbSetterContext.
  const setSegments = useCallback((next: BreadcrumbSegment[]) => {
    setSegmentsState(next);
  }, []);
  return (
    <BreadcrumbSetterContext.Provider value={setSegments}>
      <BreadcrumbSegmentsContext.Provider value={segments}>
        <div className="flex min-h-full flex-col">
          <header className="sticky top-0 z-40 flex h-14 items-center border-b border-border bg-background">
            <Link
              to="/"
              className="flex h-14 items-center gap-2 border-r border-border bg-muted px-4 text-sm font-semibold text-foreground"
            >
              <BrandMark />
              {strings.appName}
            </Link>
            <div className="flex min-w-0 flex-1 items-center px-4">
              <Breadcrumbs />
            </div>
            <div className="px-4">
              <ThemeToggle />
            </div>
          </header>
          <main className="mx-auto w-full max-w-[80rem] px-6 py-6">{children}</main>
        </div>
      </BreadcrumbSegmentsContext.Provider>
    </BreadcrumbSetterContext.Provider>
  );
}
