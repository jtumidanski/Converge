import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";

interface ProgressBarProps {
  /** 0-100. */
  value: number;
  /** Accessible name and visible caption. */
  label: string;
  indeterminate?: boolean;
  className?: string;
}

/** ProgressBar pairs a thin shadcn Progress with its caption. */
export function ProgressBar({ value, label, indeterminate = false, className }: ProgressBarProps) {
  return (
    <div className={cn("flex min-w-[9rem] flex-col gap-1", className)}>
      <span className="text-xs text-muted-foreground">{label}</span>
      <Progress
        aria-label={label}
        value={indeterminate ? undefined : value}
        className={cn("h-1.5", indeterminate && "animate-pulse")}
      />
    </div>
  );
}
