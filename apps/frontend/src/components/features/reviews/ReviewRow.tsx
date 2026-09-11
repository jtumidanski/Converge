import { useState, type MouseEvent } from "react";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { DiscardDialog } from "@/components/features/reviews/DiscardDialog";
import { ReviewProgressCell } from "@/components/features/reviews/ReviewProgressCell";
import { timeLeft } from "@/lib/timeLeft";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";
import type { Review } from "@/types/models/review";

const DOT: Record<string, string> = {
  READY: "bg-green-500",
  CREATING: "bg-amber-500",
  CONFLICTED: "bg-destructive",
  FAILED: "bg-destructive",
};

interface ReviewRowProps {
  review: Review;
  onOpen: (id: string) => void;
  onDiscard: (id: string) => void;
  pending: boolean;
}

export function ReviewRow({ review, onOpen, onDiscard, pending }: ReviewRowProps) {
  const [confirming, setConfirming] = useState(false);
  const { status, repository, provider, baseBranch, changes, totals, expiresAt } =
    review.attributes;
  const building = status === "CREATING";

  // The row is clickable, but the actions cell is not part of that target:
  // closest("[data-actions]") is why a Discard click never also navigates.
  function onRowClick(event: MouseEvent<HTMLTableRowElement>): void {
    if (building) return;
    if ((event.target as HTMLElement).closest("[data-actions]")) return;
    onOpen(review.id);
  }

  return (
    <>
      <TableRow
        onClick={onRowClick}
        className={cn(!building && "cursor-pointer")}
        aria-disabled={building || undefined}
      >
        <TableCell className="w-6">
          <span className={cn("inline-block h-2 w-2 rounded-full", DOT[status] ?? "bg-muted")} />
        </TableCell>
        <TableCell>
          <div className="flex items-baseline gap-2">
            <span className="font-medium text-foreground">{repository}</span>
            <span className="text-xs text-muted-foreground">
              {provider} · {baseBranch}
            </span>
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="font-mono">{changes.map((n) => `#${n}`).join(" · ")}</span>
            {totals ? (
              <span>
                {totals.files} files · <span className="text-foreground">+{totals.additions}</span>{" "}
                <span className="text-destructive">−{totals.deletions}</span>
              </span>
            ) : null}
          </div>
        </TableCell>
        <TableCell className="w-56">
          <ReviewProgressCell review={review} />
        </TableCell>
        <TableCell className="w-28 text-xs text-muted-foreground">
          {building ? "—" : timeLeft(expiresAt)}
        </TableCell>
        <TableCell className="w-48 text-right" data-actions="">
          {building ? (
            <Button size="sm" variant="outline" disabled>
              {strings.open}
            </Button>
          ) : (
            <div className="flex justify-end gap-2">
              <Button
                size="sm"
                variant="ghost"
                disabled={pending}
                onClick={() => setConfirming(true)}
              >
                {strings.discard}
              </Button>
              <Button size="sm" onClick={() => onOpen(review.id)}>
                {status === "READY" ? strings.resume : strings.inspect}
              </Button>
            </div>
          )}
        </TableCell>
      </TableRow>
      <DiscardDialog
        open={confirming}
        onOpenChange={setConfirming}
        pending={pending}
        onConfirm={() => {
          setConfirming(false);
          onDiscard(review.id);
        }}
      />
    </>
  );
}
