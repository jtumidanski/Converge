import { Suspense, lazy, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { EmptyState } from "@/components/common/EmptyState";
import { Skeleton } from "@/components/ui/skeleton";
import { ReviewStatus } from "@/components/features/review/ReviewStatus";
import { ReviewErrorPanel } from "@/components/features/review/ReviewErrorPanel";
import { ReviewHeader } from "@/components/features/review/ReviewHeader";
import { FileTree } from "@/components/features/review/FileTree";
import {
  useFinishReview,
  useReview,
  useReviewFile,
  useReviewFiles,
} from "@/lib/hooks/api/useReviews";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { ReviewStatus as ReviewStatusValue } from "@/types/models/review";

/**
 * assertUnreachable makes the status switch below exhaustive over ReviewStatus:
 * adding a new status without handling it here is a compile error, not a
 * silent fall-through into the diff layout (ruling R55).
 */
function assertUnreachable(status: never): never {
  throw new Error(`Unhandled review status: ${String(status)}`);
}

// Shiki is heavy; keep the diff renderer out of the initial bundle.
const FileDiff = lazy(async () => ({
  default: (await import("@/components/features/review/FileDiff")).FileDiff,
}));

export function ReviewPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const review = useReview(id);
  const status = review.data?.attributes.status;
  const files = useReviewFiles(id, status === "READY");
  // Auto-select the first file once the list loads, without setState-in-effect: derive
  // it during render (see SelectRepositoryPage.tsx). Once the user picks a file
  // explicitly, `explicitPath` wins and a refetch of the file list can never clobber it.
  const [explicitPath, setExplicitPath] = useState<string | undefined>(undefined);
  const selectedPath = explicitPath ?? files.data?.[0]?.attributes.path;
  const fileDiff = useReviewFile(id, selectedPath);
  // Finish Review and Discard Review are the same backend operation: a single
  // DELETE /api/reviews/{id} (review/service.go's Service.Finish). There is no
  // separate discard endpoint, so both user-facing actions call this one mutation;
  // only the button label and layout differ depending on whether the review
  // finished successfully or errored out.
  const finishOrDiscard = useFinishReview();

  async function closeReview() {
    if (!id) return;
    try {
      await finishOrDiscard.mutateAsync(id);
      navigate("/");
    } catch (error: unknown) {
      toast.error(messageFor(error, "The review could not be closed."));
    }
  }

  if (review.isError) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <ErrorBanner
          title="Could not load this review"
          detail={messageFor(review.error, "It may have expired.")}
          onRetry={() => void review.refetch()}
        />
      </div>
    );
  }

  if (!review.data) {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <Skeleton className="h-8 w-1/3" />
      </div>
    );
  }

  // review.data is guaranteed by the guards above; read status from it (rather
  // than the `status` computed earlier for the hooks) so the switch below is
  // exhaustive over the non-optional ReviewStatus union.
  const currentStatus: ReviewStatusValue = review.data.attributes.status;

  switch (currentStatus) {
    case "CREATING":
      return (
        <div className="mx-auto max-w-5xl p-6">
          <ReviewStatus stage={review.data.attributes.stage} />
        </div>
      );

    case "CONFLICTED":
    case "FAILED":
      return (
        <div className="mx-auto max-w-3xl p-6">
          <ReviewErrorPanel
            review={review.data}
            onDiscard={() => void closeReview()}
            discarding={finishOrDiscard.isPending}
          />
        </div>
      );

    // FINISHED and EXPIRED both mean the review's workspace is already gone:
    // FINISHED is normally transient (Finish navigates away immediately), but
    // a stale poll can still surface it, and EXPIRED is reachable directly via
    // an old review URL. Neither should render the diff layout or offer
    // Finish Review (ruling R55).
    case "FINISHED":
    case "EXPIRED":
      return (
        <div className="mx-auto flex max-w-3xl flex-col items-center gap-4 p-6">
          <EmptyState
            title={strings.reviewUnavailableTitle}
            description={strings.reviewUnavailableDescription}
          />
          <Button onClick={() => navigate("/")}>{strings.startNewReview}</Button>
        </div>
      );

    case "READY":
      return (
        <div className="mx-auto flex max-w-7xl flex-col gap-4 p-6">
          <ReviewHeader
            review={review.data}
            onFinish={() => void closeReview()}
            finishing={finishOrDiscard.isPending}
          />
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-[18rem_1fr]">
            <aside className="lg:sticky lg:top-4 lg:self-start">
              {files.isError ? (
                <ErrorBanner
                  title="Could not load the file list"
                  detail={messageFor(files.error, "Try again in a moment.")}
                  onRetry={() => void files.refetch()}
                />
              ) : files.isLoading ? (
                <div className="space-y-2">
                  {[0, 1, 2, 3].map((row) => (
                    <Skeleton key={row} className="h-8 w-full" />
                  ))}
                </div>
              ) : (
                <FileTree
                  files={files.data ?? []}
                  selectedPath={selectedPath}
                  onSelect={setExplicitPath}
                />
              )}
            </aside>
            <section>
              {files.isError ? null : files.data && files.data.length === 0 ? (
                <EmptyState
                  title="No file changes"
                  description="The selected PRs/MRs produce no net change."
                />
              ) : fileDiff.isError ? (
                <ErrorBanner
                  title="Could not load this file's diff"
                  detail={messageFor(fileDiff.error, "Try again in a moment.")}
                  onRetry={() => void fileDiff.refetch()}
                />
              ) : fileDiff.isLoading || !fileDiff.data ? (
                <Skeleton className="h-96 w-full" />
              ) : (
                <Suspense fallback={<Skeleton className="h-96 w-full" />}>
                  <FileDiff file={fileDiff.data} />
                </Suspense>
              )}
            </section>
          </div>
        </div>
      );

    default:
      return assertUnreachable(currentStatus);
  }
}
