import { Skeleton } from "@/components/ui/skeleton";

interface ReviewStatusProps {
  stage: string | null;
}

/** stageLabel converts a backend stage into product vocabulary (FR-10.11). */
function stageLabel(stage: string | null): string {
  if (!stage) return "Building the review";
  if (stage.startsWith("applying:")) return `Applying #${stage.slice("applying:".length)}`;
  switch (stage) {
    case "resolving":
      return "Resolving PRs/MRs";
    case "updating-repository":
      return "Updating repository";
    case "creating-workspace":
      return "Preparing review";
    case "diffing":
      return "Computing the combined diff";
    default:
      return "Building the review";
  }
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
