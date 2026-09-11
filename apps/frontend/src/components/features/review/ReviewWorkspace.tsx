import type { ReactNode } from "react";

/**
 * ReviewWorkspace is the fixed-height split frame: 280 px of tree beside the
 * diff, each side scrolling independently, together filling the viewport below
 * the status line. min-h keeps it usable on a short viewport, where the page
 * scrolls as a whole instead.
 */
export function ReviewWorkspace({ children }: { children: ReactNode }) {
  return (
    <div className="grid h-[calc(100vh-14rem)] min-h-[24rem] grid-cols-[280px_1fr] overflow-hidden rounded-lg border border-border bg-card">
      {children}
    </div>
  );
}
