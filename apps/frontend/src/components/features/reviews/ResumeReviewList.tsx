import { EmptyState } from "@/components/common/EmptyState";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import { ResumeReviewRow } from "@/components/features/reviews/ResumeReviewRow";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ResumeReviewListProps {
  reviews: Review[];
  /** loading is the query's isLoading -- a first load only, never a background refetch. */
  loading: boolean;
  error?: unknown;
  onRetry: () => void;
  /** pendingId is the id of the review whose discard is in flight, if any. */
  pendingId?: string | undefined;
  onResume: (id: string) => void;
  onDiscard: (id: string) => void;
}

/** body renders exactly one of error, loading, empty, or rows -- in that order. */
function body(props: ResumeReviewListProps) {
  const { reviews, loading, error, onRetry, pendingId, onResume, onDiscard } = props;

  if (error !== undefined) {
    return (
      <ErrorBanner
        title={strings.reviewsUnavailableTitle}
        detail={messageFor(error, "Try again in a moment.")}
        onRetry={onRetry}
      />
    );
  }

  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((row) => (
          <Skeleton key={row} className="h-24 w-full" />
        ))}
      </div>
    );
  }

  if (reviews.length === 0) {
    return (
      <EmptyState
        title={strings.noReviewsInProgressTitle}
        description={strings.noReviewsInProgressDescription}
      />
    );
  }

  return (
    <ul className="flex flex-col gap-2">
      {reviews.map((review) => (
        <ResumeReviewRow
          key={review.id}
          review={review}
          pending={pendingId === review.id}
          onResume={onResume}
          onDiscard={onDiscard}
        />
      ))}
    </ul>
  );
}

export function ResumeReviewList(props: ResumeReviewListProps) {
  const showCount = props.error === undefined && !props.loading && props.reviews.length > 0;
  return (
    <section className="flex flex-col gap-3" aria-labelledby="resume-review-heading">
      <div className="flex items-center justify-between gap-2">
        <h2 id="resume-review-heading" className="text-base font-semibold text-foreground">
          {strings.resumeReview}
        </h2>
        {showCount ? (
          <span
            className="text-sm text-muted-foreground"
            aria-label={`${props.reviews.length} ${strings.reviewsInProgressCount}`}
          >
            {props.reviews.length}
          </span>
        ) : null}
      </div>
      {body(props)}
    </section>
  );
}
