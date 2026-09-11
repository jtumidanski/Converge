import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { EmptyState } from "@/components/common/EmptyState";
import { Skeleton } from "@/components/ui/skeleton";
import { ReviewStatus } from "@/components/features/review/ReviewStatus";
import { ReviewErrorPanel } from "@/components/features/review/ReviewErrorPanel";
import { ReviewStatusLine } from "@/components/features/review/ReviewStatusLine";
import { ReviewWorkspace } from "@/components/features/review/ReviewWorkspace";
import { FileTree } from "@/components/features/review/FileTree";
import { DiffPane } from "@/components/features/review/DiffPane";
import {
  useFinishReview,
  useReview,
  useReviewFile,
  useReviewFiles,
} from "@/lib/hooks/api/useReviews";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { useRepository } from "@/lib/hooks/api/useRepositories";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";
import { useStore } from "@/lib/storage/store";
import { clearViewed, toggleViewed, viewedStore } from "@/lib/storage/viewed";
import { buildTree, flattenVisible } from "@/lib/review/fileTree";
import { openInProviderHref } from "@/lib/review/providerLink";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { ReviewStatus as ReviewStatusValue } from "@/types/models/review";

/**
 * assertUnreachable makes the status switch exhaustive over ReviewStatus:
 * adding a status without handling it is a compile error, not a silent
 * fall-through into the diff layout.
 */
function assertUnreachable(status: never): never {
  throw new Error(`Unhandled review status: ${String(status)}`);
}

const MAX_BREADCRUMB_CHANGES = 5;

export function ReviewPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const review = useReview(id);
  const status = review.data?.attributes.status;
  const files = useReviewFiles(id, status === "READY");
  const [explicitPath, setExplicitPath] = useState<string | undefined>(undefined);
  const [fromKeyboard, setFromKeyboard] = useState(false);
  // Read-only binding: toggleViewed writes through the same store, so the
  // setter half of the tuple would be a second way to do one thing.
  const [viewedPaths] = useStore(viewedStore(id ?? ""));
  const providers = useProviders();
  const repositoryQuery = useRepository(
    review.data?.attributes.provider,
    review.data?.attributes.repository ?? "",
    Boolean(review.data),
  );
  // Finish Review and Discard are the same backend call (DELETE
  // /api/reviews/{id}); only the label and the confirmation differ.
  const finishOrDiscard = useFinishReview();

  const fileList = useMemo(() => files.data ?? [], [files.data]);
  const order = useMemo(() => flattenVisible(buildTree(fileList), new Set()), [fileList]);
  const selectedPath = explicitPath ?? order[0];
  const fileDiff = useReviewFile(id, selectedPath);
  const viewedSet = useMemo(() => new Set(viewedPaths), [viewedPaths]);

  const changeNumbers = review.data?.attributes.changes ?? [];
  // changeNumbers is a fresh array each render; key the breadcrumb memo on
  // this string instead so the effect doesn't republish every render.
  const changeNumbersKey = changeNumbers.join(",");
  useBreadcrumbs(
    useMemo(() => {
      const shown = changeNumbers
        .slice(0, MAX_BREADCRUMB_CHANGES)
        .map((n) => `#${n}`)
        .join(" · ");
      const label = changeNumbers.length > MAX_BREADCRUMB_CHANGES ? `${shown} · …` : shown;
      return [
        { label: strings.reviews, to: "/" },
        { label: review.data?.attributes.repository ?? "" },
        { label },
      ];
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [review.data?.attributes.repository, changeNumbersKey]),
  );

  const index = selectedPath === undefined ? -1 : order.indexOf(selectedPath);
  const isLast = index >= 0 && index === order.length - 1;
  const nextPath = order.length === 0 ? undefined : order[(index + 1) % order.length];
  const previousPath =
    order.length === 0 ? undefined : order[(index - 1 + order.length) % order.length];

  function select(path: string | undefined, source: "pointer" | "keyboard"): void {
    if (path === undefined) return;
    setExplicitPath(path);
    setFromKeyboard(source === "keyboard");
  }

  function toggleSelectedViewed(): void {
    if (id === undefined || selectedPath === undefined) return;
    toggleViewed(id, selectedPath);
  }

  useHotkeys(
    {
      j: () => select(nextPath, "keyboard"),
      k: () => select(previousPath, "keyboard"),
      v: toggleSelectedViewed,
    },
    { enabled: status === "READY" },
  );

  async function closeReview(): Promise<void> {
    if (!id) return;
    try {
      await finishOrDiscard.mutateAsync(id);
      clearViewed(id);
      navigate("/");
    } catch (error: unknown) {
      toast.error(messageFor(error, "The review could not be closed."));
    }
  }

  if (review.isError) {
    return (
      <ErrorBanner
        title="Could not load this review"
        detail={messageFor(review.error, "It may have expired.")}
        onRetry={() => void review.refetch()}
      />
    );
  }
  if (!review.data) return <Skeleton className="h-8 w-1/3" />;

  const currentStatus: ReviewStatusValue = review.data.attributes.status;

  switch (currentStatus) {
    case "CREATING":
      return <ReviewStatus stage={review.data.attributes.stage} />;

    case "CONFLICTED":
    case "FAILED":
      return (
        <ReviewErrorPanel
          review={review.data}
          onDiscard={() => void closeReview()}
          discarding={finishOrDiscard.isPending}
        />
      );

    // FINISHED and EXPIRED both mean the workspace is already gone; neither
    // should render the diff layout or offer Finish Review.
    case "FINISHED":
    case "EXPIRED":
      return (
        <div className="flex flex-col items-center gap-4">
          <EmptyState
            title={strings.reviewUnavailableTitle}
            description={strings.reviewUnavailableDescription}
          />
          <Button onClick={() => navigate("/")}>{strings.startNewReview}</Button>
        </div>
      );

    case "READY": {
      const providerName =
        providers.data?.find((p) => p.id === review.data?.attributes.provider)?.attributes
          .displayName ?? review.data.attributes.provider;
      const href = openInProviderHref(review.data, repositoryQuery.data?.attributes.webUrl);
      return (
        <div className="flex flex-col gap-4">
          <ReviewStatusLine
            review={review.data}
            files={fileList}
            viewed={viewedSet}
            onFinish={() => void closeReview()}
            onDiscard={() => void closeReview()}
            pending={finishOrDiscard.isPending}
          />
          {files.isError ? (
            <ErrorBanner
              title="Could not load the file list"
              detail={messageFor(files.error, "Try again in a moment.")}
              onRetry={() => void files.refetch()}
            />
          ) : !files.isLoading && fileList.length === 0 ? (
            <EmptyState
              title="No file changes"
              description="The selected PRs/MRs produce no net change."
            />
          ) : (
            <ReviewWorkspace>
              <FileTree
                files={fileList}
                viewed={viewedSet}
                selectedPath={selectedPath}
                onSelect={select}
                onToggleViewed={(path) => id && toggleViewed(id, path)}
                scrollSelectionIntoView={fromKeyboard}
              />
              <DiffPane
                fileDiff={fileDiff.data}
                loading={fileDiff.isLoading || files.isLoading}
                {...(fileDiff.isError ? { error: fileDiff.error } : {})}
                onRetry={() => void fileDiff.refetch()}
                href={href}
                providerName={providerName}
                viewed={selectedPath !== undefined && viewedSet.has(selectedPath)}
                onToggleViewed={toggleSelectedViewed}
                index={Math.max(index, 0)}
                total={order.length}
                nextName={nextPath}
                isLast={isLast}
                onNext={() => select(nextPath, "keyboard")}
              />
            </ReviewWorkspace>
          )}
        </div>
      );
    }

    default:
      return assertUnreachable(currentStatus);
  }
}
