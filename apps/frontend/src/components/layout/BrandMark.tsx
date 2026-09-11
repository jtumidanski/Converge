import { cn } from "@/lib/utils";

/** BrandMark is the square logo tile in the top bar's brand block. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex h-6 w-6 items-center justify-center rounded-[0.3rem] bg-foreground text-[0.7rem] font-bold text-background",
        className,
      )}
    >
      C
    </span>
  );
}
