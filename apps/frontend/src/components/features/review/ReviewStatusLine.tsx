import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ProgressBar } from "@/components/common/ProgressBar";
import { DiscardDialog } from "@/components/features/reviews/DiscardDialog";
import { IncludedChangesPopover } from "@/components/features/review/IncludedChangesPopover";
import { ticketKey } from "@/lib/changes/ticketKey";
import { viewedProgress } from "@/lib/review/progress";
import { shortSha } from "@/types/models/change";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";
import type { ReviewFile } from "@/types/models/reviewFile";

interface ReviewStatusLineProps {
  review: Review;
  files: ReviewFile[];
  viewed: ReadonlySet<string>;
  onFinish: () => void;
  onDiscard: () => void;
  pending: boolean;
}

export function ReviewStatusLine({
  review,
  files,
  viewed,
  onFinish,
  onDiscard,
  pending,
}: ReviewStatusLineProps) {
  const [confirming, setConfirming] = useState(false);
  const { status, included, baseBranch, baseSha, baseDescription, totals } = review.attributes;
  // The first key found across the included titles, so a multi-change review
  // for one ticket still reads as that ticket.
  const key = useMemo(() => {
    for (const change of included) {
      const found = ticketKey(change.title);
      if (found !== null) return found;
    }
    return null;
  }, [included]);
  const progress = viewedProgress(files, viewed);

  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-border pb-3">
      <Badge variant={status === "READY" ? "secondary" : "outline"}>{strings.statusReady}</Badge>
      {key ? <Badge>{key}</Badge> : null}
      <span className="flex items-center gap-1">
        {included.map((change) => (
          <Badge key={change.number} variant="outline" className="font-mono">
            #{change.number}
          </Badge>
        ))}
        <IncludedChangesPopover included={included} />
      </span>
      <Separator orientation="vertical" className="h-5" />
      <span className="truncate text-xs text-muted-foreground">
        {strings.base}{" "}
        <span className="font-mono text-foreground">
          {baseBranch} @ {shortSha(baseSha)}
        </span>
        {baseDescription ? ` · ${baseDescription}` : null}
      </span>
      <span className="ml-auto flex items-center gap-4">
        {totals ? (
          <span className="text-xs text-muted-foreground">
            {totals.files} files · <span className="text-foreground">+{totals.additions}</span>{" "}
            <span className="text-destructive">−{totals.deletions}</span>
          </span>
        ) : null}
        <ProgressBar
          value={progress.percent}
          label={`${progress.viewed} / ${progress.total} viewed`}
        />
        <Button variant="ghost" size="sm" disabled={pending} onClick={() => setConfirming(true)}>
          {strings.discard}
        </Button>
        <Button size="sm" disabled={pending} onClick={onFinish}>
          {strings.finishReview}
        </Button>
      </span>
      <DiscardDialog
        open={confirming}
        onOpenChange={setConfirming}
        pending={pending}
        onConfirm={() => {
          setConfirming(false);
          onDiscard();
        }}
      />
    </div>
  );
}
