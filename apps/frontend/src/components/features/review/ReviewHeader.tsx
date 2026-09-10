import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ReviewHeaderProps {
  review: Review;
  onFinish: () => void;
  finishing: boolean;
}

export function ReviewHeader({ review, onFinish, finishing }: ReviewHeaderProps) {
  const { repository, baseBranch, baseSha, baseDescription, included, totals } = review.attributes;
  return (
    <header className="flex flex-col gap-3 border-b border-border pb-4">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-foreground">{repository}</h1>
          <p className="text-sm text-muted-foreground">
            {strings.base}: {baseBranch} @ {baseSha ? baseSha.slice(0, 7) : "unknown"}
            {baseDescription ? ` · ${baseDescription}` : ""}
          </p>
        </div>
        <Button onClick={onFinish} disabled={finishing}>
          {finishing ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {strings.finishReview}
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium text-foreground">{strings.includedChanges}:</span>
        {included.map((change) => (
          <a
            key={change.number}
            href={change.webUrl}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-primary underline-offset-2 hover:underline"
          >
            #{change.number} {change.title}
          </a>
        ))}
      </div>
      {totals ? (
        <div className="flex items-center gap-2">
          <Badge variant="secondary">{totals.files} files</Badge>
          <Badge variant="secondary">+{totals.additions}</Badge>
          <Badge variant="secondary">−{totals.deletions}</Badge>
        </div>
      ) : null}
    </header>
  );
}
