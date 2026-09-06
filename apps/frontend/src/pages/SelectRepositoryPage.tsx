import { useState } from "react";
import { useNavigate } from "react-router";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { ProviderPicker } from "@/components/features/providers/ProviderPicker";
import { RepositoryList } from "@/components/features/repositories/RepositoryList";
import { ManualRepositoryForm } from "@/components/features/repositories/ManualRepositoryForm";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { useRepositories } from "@/lib/hooks/api/useRepositories";
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
