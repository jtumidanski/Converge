import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { ChangeSearch } from "@/components/features/changes/ChangeSearch";
import { ChangeTable } from "@/components/features/changes/ChangeTable";
import { SelectionBar } from "@/components/features/changes/SelectionBar";
import { Input } from "@/components/ui/input";
import { useChanges } from "@/lib/hooks/api/useChanges";
import type { ChangeListParams } from "@/services/api";
import { useRepository } from "@/lib/hooks/api/useRepositories";
import { useCreateReview } from "@/lib/hooks/api/useReviews";
import type { CreateReviewRequest } from "@/types/models/review";
import { useSelection } from "@/lib/hooks/useSelection";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

export function SelectChangesPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const providerId = params.get("provider") ?? undefined;
  const repository = params.get("repo") ?? undefined;

  // baseBranch tracks whether the user has typed a value yet; once they have,
  // the default from the repository must not clobber it.
  const [baseBranchState, setBaseBranchState] = useState<{ edited: boolean; value: string }>({
    edited: false,
    value: "",
  });
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [createError, setCreateError] = useState<string | null>(null);

  const repositoryQuery = useRepository(providerId, repository ?? "", Boolean(repository));
  const defaultBranch = repositoryQuery.data?.attributes.defaultBranch;

  // Seed baseBranch from the repository's default once it loads, without
  // setState-in-effect: derive it during render, per useSelection.ts and
  // SelectRepositoryPage.tsx's precedent.
  const baseBranch = baseBranchState.edited ? baseBranchState.value : (defaultBranch ?? baseBranchState.value);
  if (!baseBranchState.edited && defaultBranch && defaultBranch !== baseBranchState.value) {
    setBaseBranchState({ edited: false, value: defaultBranch });
  }

  const selection = useSelection(`converge:selection:${providerId ?? ""}/${repository ?? ""}`);
  const changeParams: ChangeListParams = baseBranch ? { target: baseBranch, search, page } : { search, page };
  // Wait for the repository lookup to settle (succeed or fail) before firing the
  // changes request, so we never issue an unfiltered "all merged changes" request
  // that gets immediately discarded once the default branch resolves. Gate on
  // settled, not on success: if the lookup fails, baseBranch stays "" and changes
  // must still be fetched untargeted — the user sees the repository error separately.
  const changes = useChanges(providerId, repository, changeParams, !repositoryQuery.isPending);
  const createReview = useCreateReview();

  if (!providerId || !repository) {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <ErrorBanner title="Missing selection" detail="Go back and choose a provider and repository." />
      </div>
    );
  }

  async function build() {
    setCreateError(null);
    try {
      const request: CreateReviewRequest = baseBranch
        ? { provider: providerId as string, repository: repository as string, baseBranch, changes: selection.numbers }
        : { provider: providerId as string, repository: repository as string, changes: selection.numbers };
      const review = await createReview.mutateAsync(request);
      selection.clear();
      navigate(`/reviews/${review.id}`);
    } catch (error: unknown) {
      const detail = messageFor(error, "The review could not be started.");
      setCreateError(detail);
      toast.error(detail);
    }
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4 p-6">
      <PageHeader title={repository} description={`${strings.provider}: ${providerId}`} />
      <div className="flex flex-wrap items-end gap-4">
        <div className="flex flex-col gap-1">
          <label htmlFor="base-branch" className="text-sm font-medium text-foreground">
            {strings.base}
          </label>
          <Input
            id="base-branch"
            className="w-64"
            value={baseBranch}
            onChange={(event) => {
              setBaseBranchState({ edited: true, value: event.target.value });
              setPage(1);
            }}
          />
        </div>
        <ChangeSearch
          value={search}
          onChange={(value) => {
            setSearch(value);
            setPage(1);
          }}
        />
      </div>
      {createError ? <ErrorBanner title="Could not start the review" detail={createError} /> : null}
      {changes.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.includedChanges.toLowerCase()}`}
          detail={messageFor(changes.error, "Try again in a moment.")}
          onRetry={() => void changes.refetch()}
        />
      ) : (
        <ChangeTable
          changes={changes.data?.items ?? []}
          loading={changes.isLoading}
          isSelected={selection.isSelected}
          onToggle={selection.toggle}
        />
      )}
      <Pagination
        page={page}
        hasNext={changes.data?.page?.hasNext ?? false}
        onChange={setPage}
        disabled={changes.isFetching}
      />
      <SelectionBar
        count={selection.count}
        building={createReview.isPending}
        onBuild={() => void build()}
        onClear={selection.clear}
      />
    </div>
  );
}
