import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { ProviderPicker } from "@/components/features/providers/ProviderPicker";
import { RepositoryList } from "@/components/features/repositories/RepositoryList";
import { ManualRepositoryForm } from "@/components/features/repositories/ManualRepositoryForm";
import { ResumeReviewList } from "@/components/features/reviews/ResumeReviewList";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { useRepositories } from "@/lib/hooks/api/useRepositories";
import { useFinishReview, useReviews } from "@/lib/hooks/api/useReviews";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { Repository } from "@/types/models/repository";

export function SelectRepositoryPage() {
  const navigate = useNavigate();
  const [selectedProviderId, setSelectedProviderId] = useState<string | undefined>(undefined);
  const [page, setPage] = useState(1);
  const providers = useProviders();
  // Default to the first provider once the list loads, without setState-in-effect:
  // derive it during render rather than syncing state to an external source.
  const providerId = selectedProviderId ?? providers.data?.[0]?.id;
  const repositories = useRepositories(providerId, { page });

  const reviews = useReviews();
  // Discard here and Finish Review on ReviewPage are the same backend
  // operation (DELETE /api/reviews/{id}); this list simply calls it from
  // outside the review. Unlike ReviewPage's closeReview it does not navigate --
  // the reviewer stays on `/` and the mutation's onSettled invalidation of
  // reviewKeys.lists() removes the row.
  const discardReview = useFinishReview();

  async function discard(id: string) {
    try {
      await discardReview.mutateAsync(id);
    } catch (error: unknown) {
      toast.error(messageFor(error, strings.reviewDiscardFailed));
    }
  }

  function goToChanges(repository: Repository) {
    if (!providerId) return;
    const search = new URLSearchParams({ provider: providerId, repo: repository.id });
    navigate(`/select?${search.toString()}`);
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <PageHeader
        title={strings.combinedReview}
        description={`Choose a ${strings.provider.toLowerCase()} and a ${strings.repository.toLowerCase()} to start.`}
      />
      <ResumeReviewList
        reviews={reviews.data ?? []}
        // isLoading, not isFetching: a background poll must not replace
        // rendered rows with skeletons (FR-5.5).
        loading={reviews.isLoading}
        error={reviews.isError ? reviews.error : undefined}
        onRetry={() => void reviews.refetch()}
        // The mutation itself holds the in-flight variables, so there is no
        // second source of truth to fall out of sync (design 2.3).
        pendingId={discardReview.isPending ? discardReview.variables : undefined}
        onResume={(id) => navigate(`/reviews/${id}`)}
        onDiscard={(id) => void discard(id)}
      />
      {providers.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.provider.toLowerCase()}s`}
          detail={messageFor(providers.error, "Try again in a moment.")}
          onRetry={() => void providers.refetch()}
        />
      ) : null}
      <ProviderPicker
        providers={providers.data ?? []}
        value={providerId}
        onChange={(id) => {
          setSelectedProviderId(id);
          setPage(1);
        }}
        loading={providers.isLoading}
      />
      <ManualRepositoryForm providerId={providerId} onResolved={goToChanges} />
      {repositories.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.repository.toLowerCase()}s`}
          detail={messageFor(repositories.error, "Try again in a moment.")}
          onRetry={() => void repositories.refetch()}
        />
      ) : (
        <>
          <RepositoryList
            repositories={repositories.data?.items ?? []}
            // A query disabled by `enabled: Boolean(providerId)` reports
            // isLoading=false with data=undefined, so without the second term
            // the list would claim the token sees no repositories before any
            // request was issued -- transiently on every load, and forever
            // when no provider is configured.
            loading={repositories.isLoading || !providerId}
            onSelect={goToChanges}
          />
          <Pagination
            page={page}
            hasNext={repositories.data?.page?.hasNext ?? false}
            onChange={setPage}
            disabled={repositories.isFetching}
          />
        </>
      )}
    </div>
  );
}
