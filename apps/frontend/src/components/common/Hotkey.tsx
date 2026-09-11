import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** Hotkey renders the <kbd> chip shown beside a shortcut-backed action. */
export function Hotkey({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded border border-border bg-muted px-1 font-mono text-[0.6875rem] text-muted-foreground",
        className,
      )}
    >
      {children}
    </kbd>
  );
}
