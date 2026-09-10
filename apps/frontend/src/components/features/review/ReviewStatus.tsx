import { Skeleton } from "@/components/ui/skeleton";
import { stageLabel } from "@/lib/stageLabel";

interface ReviewStatusProps {
  stage: string | null;
}

export function ReviewStatus({ stage }: ReviewStatusProps) {
  return (
    <div className="flex flex-col gap-4" aria-live="polite">
      <p className="text-sm font-medium text-foreground">{stageLabel(stage)}</p>
      <Skeleton className="h-8 w-1/3" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
