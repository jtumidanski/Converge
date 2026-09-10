import { useState } from "react";
import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { expiryLabel, relativeTime } from "@/lib/relativeTime";
import { stageLabel } from "@/lib/stageLabel";
import { strings } from "@/lib/strings";
import type { Review, ReviewErrorPayload } from "@/types/models/review";

interface ResumeReviewRowProps {
  review: Review;
  /** pending is true only for the row whose discard is in flight (FR-4.4). */
  pending: boolean;
  onResume: (id: string) => void;
  onDiscard: (id: string) => void;
}

type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

/**
 * statusPresentation deliberately takes a string rather than the ReviewStatus
 * union: this list must degrade on a status it does not recognise rather than
 * fail to render (FR-2.4, NFR-5). ReviewPage's exhaustive switch is the right
 * pattern there because it picks a layout; here status is data.
 */
function statusPresentation(status: string): { label: string; variant: BadgeVariant } {
  switch (status) {
    case "READY":
      return { label: strings.statusReady, variant: "default" };
    case "CREATING":
      return { label: strings.statusBuilding, variant: "secondary" };
    case "CONFLICTED":
      return { label: strings.conflict, variant: "destructive" };
    case "FAILED":
      return { label: strings.statusFailed, variant: "destructive" };
    default:
      return { label: status, variant: "outline" };
  }
}

/** errorSummary renders one line: the code, and the change it concerns when known. */
function errorSummary(error: ReviewErrorPayload): string {
  return error.change === undefined ? error.code : `${error.code} on #${error.change}`;
}

export function ResumeReviewRow({ review, pending, onResume, onDiscard }: ResumeReviewRowProps) {
  const [confirming, setConfirming] = useState(false);
  const { status, stage, provider, repository, changes, totals, error, createdAt, expiresAt } =
    review.attributes;
  const presentation = statusPresentation(status);
  const changeLabel = changes.map((number) => `#${number}`).join(", ");
  const expiry = expiryLabel(expiresAt);
  const expiryText =
    expiry.text === "" || expiry.text === strings.expired
      ? expiry.text
      : `${strings.expires} ${expiry.text}`;

  function confirmDiscard() {
    // Clear the armed state before firing: on success the row unmounts, and on
    // failure the reviewer should see the toast against a resting row.
    setConfirming(false);
    onDiscard(review.id);
  }

  return (
    <li className="flex flex-col gap-2 rounded-md border border-border p-4">
      <div className="flex items-center gap-2">
        <Badge variant={presentation.variant}>{presentation.label}</Badge>
        <span className="truncate text-sm text-muted-foreground">{provider}</span>
        <span className="truncate text-sm font-medium text-foreground">{repository}</span>
      </div>

      <p className="text-sm text-muted-foreground">
        <span>{changeLabel}</span>
        {status === "CREATING" ? (
          <>
            {" · "}
            <span aria-live="polite">{stageLabel(stage)}</span>
          </>
        ) : null}
        {totals ? (
          <span>
            {" · "}
            {totals.files} files · +{totals.additions} −{totals.deletions}
          </span>
        ) : null}
        {error ? (
          <span>
            {" · "}
            {errorSummary(error)}
          </span>
        ) : null}
      </p>

      <p className="text-sm text-muted-foreground">
        {strings.started} {relativeTime(createdAt)}
        {expiryText ? (
          <>
            {" · "}
            <span
              className={expiry.nearExpiry ? "font-medium text-destructive" : undefined}
              {...(expiry.nearExpiry ? { role: "status" } : {})}
            >
              {expiryText}
            </span>
          </>
        ) : null}
      </p>

      {confirming ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <span className="mr-auto text-sm text-foreground">
            {strings.discardReview}: {repository} ({changeLabel})? {strings.discardConfirmSuffix}
          </span>
          <Button variant="outline" size="sm" onClick={() => setConfirming(false)}>
            {strings.cancel}
          </Button>
          <Button variant="destructive" size="sm" autoFocus onClick={confirmDiscard}>
            {strings.discard}
          </Button>
        </div>
      ) : (
        <div className="flex items-center justify-end gap-2">
          <Button
            size="sm"
            disabled={pending}
            aria-label={`${strings.resume} review of ${repository} (${changeLabel})`}
            onClick={() => onResume(review.id)}
          >
            {strings.resume}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={pending}
            aria-label={`${strings.discard} review of ${repository} (${changeLabel})`}
            onClick={() => setConfirming(true)}
          >
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {strings.discard}
          </Button>
        </div>
      )}
    </li>
  );
}
