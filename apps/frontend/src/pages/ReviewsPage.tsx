import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ReviewsTable } from "@/components/features/reviews/ReviewsTable";
import { NewReviewSheet } from "@/components/features/newReview/NewReviewSheet";
import { useFinishReview, useReviews } from "@/lib/hooks/api/useReviews";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";
import { clearViewed, pruneViewed } from "@/lib/storage/viewed";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

export function ReviewsPage() {
  const navigate = useNavigate();
  const [sheetOpen, setSheetOpen] = useState(false);
  const reviews = useReviews();
  // Discard here and Finish Review on the review page are the same backend
  // operation (DELETE /api/reviews/{id}); the mutation's invalidation removes
  // the row, so this handler does not navigate.
  const discardReview = useFinishReview();

  useBreadcrumbs(useMemo(() => [{ label: strings.reviews }], []));

  const all = reviews.data;
  const active = useMemo(
    () =>
      (all ?? []).filter(
        (review) =>
          review.attributes.status !== "FINISHED" && review.attributes.status !== "EXPIRED",
      ),
    [all],
  );

  // Viewed state for a review the server no longer lists (swept, expired, or
  // finished in another tab) would otherwise linger in localStorage forever.
  useEffect(() => {
    if (all === undefined) return;
    pruneViewed(all.map((review) => review.id));
  }, [all]);

  useHotkeys({ n: () => setSheetOpen(true) }, { enabled: !sheetOpen });

  async function discard(id: string): Promise<void> {
    try {
      await discardReview.mutateAsync(id);
      clearViewed(id);
    } catch (error: unknown) {
      toast.error(messageFor(error, strings.reviewDiscardFailed));
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={strings.combinedReview} description={strings.reviewsPageDescription} />
      <ReviewsTable
        reviews={active}
        // isLoading, not isFetching: a background poll must not replace
        // rendered rows with skeletons.
        loading={reviews.isLoading}
        {...(reviews.isError ? { error: reviews.error } : {})}
        onRetry={() => void reviews.refetch()}
        onOpen={(id) => navigate(`/reviews/${id}`)}
        onDiscard={(id) => void discard(id)}
        onNewReview={() => setSheetOpen(true)}
        {...(discardReview.isPending && discardReview.variables
          ? { pendingId: discardReview.variables }
          : {})}
      />
      <NewReviewSheet open={sheetOpen} onOpenChange={setSheetOpen} />
    </div>
  );
}
