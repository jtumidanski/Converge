import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { NewReviewRow } from "@/components/features/reviews/NewReviewRow";
import { ReviewRow } from "@/components/features/reviews/ReviewRow";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ReviewsTableProps {
  reviews: Review[];
  loading: boolean;
  error?: unknown;
  onRetry?: () => void;
  onOpen: (id: string) => void;
  onDiscard: (id: string) => void;
  onNewReview: () => void;
  pendingId?: string;
}

/**
 * ReviewsTable is the root page's single card. FINISHED and EXPIRED reviews
 * are filtered out by the page before they reach here (FR-9).
 */
export function ReviewsTable({
  reviews,
  loading,
  error,
  onRetry,
  onOpen,
  onDiscard,
  onNewReview,
  pendingId,
}: ReviewsTableProps) {
  if (error) {
    return (
      <ErrorBanner
        title={strings.reviewsUnavailableTitle}
        detail={messageFor(error, "Try again in a moment.")}
        {...(onRetry ? { onRetry } : {})}
      />
    );
  }
  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="border-b border-border px-4 py-3 text-sm font-medium text-foreground">
        {strings.reviews}
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="sr-only">{strings.columnStatus}</TableHead>
            <TableHead className="sr-only">{strings.repository}</TableHead>
            <TableHead className="sr-only">{strings.columnProgress}</TableHead>
            <TableHead className="sr-only">{strings.columnExpires}</TableHead>
            <TableHead className="sr-only">{strings.columnActions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading
            ? [0, 1, 2].map((row) => (
                <TableRow key={row}>
                  <TableCell colSpan={5}>
                    <Skeleton className="h-10 w-full" />
                  </TableCell>
                </TableRow>
              ))
            : null}
          {!loading && reviews.length === 0 ? (
            <TableRow>
              <TableCell colSpan={5} className="py-6 text-center text-sm text-muted-foreground">
                {strings.noOpenReviews}
              </TableCell>
            </TableRow>
          ) : null}
          {!loading
            ? reviews.map((review) => (
                <ReviewRow
                  key={review.id}
                  review={review}
                  onOpen={onOpen}
                  onDiscard={onDiscard}
                  pending={pendingId === review.id}
                />
              ))
            : null}
          <NewReviewRow onClick={onNewReview} />
        </TableBody>
      </Table>
    </div>
  );
}
