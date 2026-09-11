import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { BaseBranchSelect } from "@/components/features/changes/BaseBranchSelect";
import { ChangeFilters } from "@/components/features/changes/ChangeFilters";
import { ChangeTable, buildRows } from "@/components/features/changes/ChangeTable";
import { SelectionBar } from "@/components/features/changes/SelectionBar";
import { useBranches } from "@/lib/hooks/api/useBranches";
import { useChanges } from "@/lib/hooks/api/useChanges";
import { useRepository } from "@/lib/hooks/api/useRepositories";
import { useCreateReview } from "@/lib/hooks/api/useReviews";
import { useSelection } from "@/lib/hooks/useSelection";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";
import { useStore } from "@/lib/storage/store";
import { changeFiltersStore } from "@/lib/storage/changeFilters";
import { recordRecent } from "@/lib/storage/recents";
import { applyOrder } from "@/lib/changes/applyOrder";
import { isDependencyBot } from "@/lib/changes/dependencyBot";
import { groupByTicket, type TicketGroup } from "@/lib/changes/groupByTicket";
import { distinctAuthors } from "@/lib/changes/authors";
import { isValidRepositoryName } from "@/lib/repositoryInput";
import type { ChangeListParams } from "@/services/api";
import type { CreateReviewRequest } from "@/types/models/review";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

/**
 * isPlausibleBranch is the client half of gitx.ValidateBranchSyntax: enough to
 * reject a nonsense ?base= without duplicating git's full ref rules, which the
 * backend applies anyway when the review is built.
 */
function isPlausibleBranch(value: string | null): boolean {
  if (value === null || value === "" || value.length > 255) return false;
  if (value.startsWith("-") || value.startsWith("/") || value.endsWith("/")) return false;
  return !/[\s~^:?*[\\]|\.\.|@\{/.test(value);
}

export function SelectChangesPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const providerId = params.get("provider") ?? undefined;
  const repository = params.get("repo") ?? undefined;
  const baseParam = params.get("base");

  const [baseState, setBaseState] = useState<{ edited: boolean; value: string }>({
    edited: false,
    value: "",
  });
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [authorFilter, setAuthorFilter] = useState<Set<string>>(new Set());
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [filters, setFilters] = useStore(changeFiltersStore);

  const repositoryQuery = useRepository(providerId, repository ?? "", Boolean(repository));
  const defaultBranch = repositoryQuery.data?.attributes.defaultBranch;

  // Seed the base from ?base= when it is syntactically usable, else from the
  // repository default, during render rather than in an effect (the existing
  // precedent in this file and useSelection.ts). An unusable ?base= is ignored
  // rather than surfaced: the backend validates the name again on build.
  const seeded = isPlausibleBranch(baseParam) ? (baseParam as string) : defaultBranch;
  const baseBranch = baseState.edited ? baseState.value : (seeded ?? baseState.value);
  if (!baseState.edited && seeded && seeded !== baseState.value) {
    setBaseState({ edited: false, value: seeded });
  }

  const selection = useSelection(`converge:selection:${providerId ?? ""}/${repository ?? ""}`);
  const changeParams: ChangeListParams = baseBranch
    ? { target: baseBranch, search, page }
    : { search, page };
  // Wait for the repository lookup to settle before firing the changes
  // request, so no unfiltered "all merged changes" request is issued and then
  // discarded once the default branch resolves.
  const changes = useChanges(providerId, repository, changeParams, !repositoryQuery.isPending);
  const createReview = useCreateReview();
  // BaseBranchSelect (Task 19) has no isError prop by design (Ruling 16): it
  // cannot distinguish a fetch failure from a genuinely empty branch list, so
  // it silently falls back to the pinned default plus a typed value either
  // way. The create page owns surfacing the failure itself, so it runs its own
  // unsearched branches query eagerly on load. BaseBranchSelect's query is
  // enabled only while the popover is open, so this is the only request until
  // the user opens it; once open with an empty search the keys match and React
  // Query serves it from this same cache entry.
  const branches = useBranches(
    providerId,
    repository,
    {},
    Boolean(providerId) && Boolean(repository),
  );

  // FR-16: the create page records the recent, not the drawer, so a deep link
  // counts the same as a trip through the drawer.
  useEffect(() => {
    if (providerId === undefined || repository === undefined || defaultBranch === undefined) return;
    recordRecent({
      provider: providerId,
      repository,
      defaultBranch,
      openedAt: new Date().toISOString(),
    });
  }, [providerId, repository, defaultBranch]);

  // FR-2 wants the provider's display name here, not its id.
  const providers = useProviders();
  const providerName =
    providers.data?.find((p) => p.id === providerId)?.attributes.displayName ?? providerId ?? "";
  useBreadcrumbs(
    useMemo(
      () => [
        { label: strings.reviews, to: "/" },
        { label: providerName },
        { label: repository ?? "" },
      ],
      [providerName, repository],
    ),
  );

  const items = useMemo(() => changes.data?.items ?? [], [changes.data]);
  const authors = useMemo(() => distinctAuthors(items), [items]);
  const afterBots = useMemo(
    () => (filters.hideBots ? items.filter((c) => !isDependencyBot(c)) : items),
    [items, filters.hideBots],
  );
  const visible = useMemo(
    () =>
      authorFilter.size === 0
        ? afterBots
        : afterBots.filter((c) => authorFilter.has(c.attributes.author)),
    [afterBots, authorFilter],
  );
  const groups = useMemo(
    () => (filters.groupByTicket ? groupByTicket(visible) : null),
    [filters.groupByTicket, visible],
  );
  const rows = useMemo(
    () => buildRows(visible, groups, items.length - afterBots.length, selection.isSelected),
    [visible, groups, items.length, afterBots.length, selection],
  );

  const allSelected =
    visible.length > 0 && visible.every((c) => selection.isSelected(c.attributes.number));
  const someSelected =
    !allSelected && visible.some((c) => selection.isSelected(c.attributes.number));

  const canBuild = selection.count > 0 && !createReview.isPending;
  useHotkeys({ Enter: () => void build() }, { enabled: canBuild && !popoverOpen });

  if (!providerId || !repository || !isValidRepositoryName(repository)) {
    return (
      <ErrorBanner
        title="Missing selection"
        detail="Go back and choose a provider and repository."
      />
    );
  }

  function toggleAuthor(author: string): void {
    setAuthorFilter((current) => {
      const next = new Set(current);
      if (next.has(author)) next.delete(author);
      else next.add(author);
      return next;
    });
  }

  function toggleGroup(group: TicketGroup, select: boolean): void {
    for (const change of group.changes) {
      if (selection.isSelected(change.attributes.number) !== select) selection.toggle(change);
    }
  }

  function toggleAll(select: boolean): void {
    for (const change of visible) {
      if (selection.isSelected(change.attributes.number) !== select) selection.toggle(change);
    }
  }

  async function build(): Promise<void> {
    setCreateError(null);
    const ordered = applyOrder(selection.selected.values()).map((c) => c.attributes.number);
    try {
      const request: CreateReviewRequest = baseBranch
        ? {
            provider: providerId as string,
            repository: repository as string,
            baseBranch,
            changes: ordered,
          }
        : { provider: providerId as string, repository: repository as string, changes: ordered };
      const review = await createReview.mutateAsync(request);
      if (defaultBranch !== undefined) {
        recordRecent({
          provider: providerId as string,
          repository: repository as string,
          defaultBranch,
          openedAt: new Date().toISOString(),
        });
      }
      selection.clear();
      navigate(`/reviews/${review.id}`);
    } catch (error: unknown) {
      const detail = messageFor(error, "The review could not be started.");
      setCreateError(detail);
      toast.error(detail);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <PageHeader
          title={repository}
          description="Select the merged changes to review together. They are applied in merge order onto the base."
        />
        <BaseBranchSelect
          providerId={providerId}
          repository={repository}
          value={baseBranch}
          defaultBranch={defaultBranch}
          onOpenChange={setPopoverOpen}
          onChange={(branch) => {
            setBaseState({ edited: true, value: branch });
            setPage(1);
            selection.clear();
          }}
        />
      </div>
      <ChangeFilters
        search={search}
        onSearchChange={(value) => {
          setSearch(value);
          setPage(1);
        }}
        authors={authors}
        activeAuthors={authorFilter}
        onToggleAuthor={toggleAuthor}
        hideBots={filters.hideBots}
        onHideBotsChange={(next) => setFilters((prev) => ({ ...prev, hideBots: next }))}
        groupByTicket={filters.groupByTicket}
        onGroupByTicketChange={(next) => setFilters((prev) => ({ ...prev, groupByTicket: next }))}
        shown={visible.length}
        total={items.length}
      />
      {createError ? <ErrorBanner title="Could not start the review" detail={createError} /> : null}
      {branches.isError ? (
        <ErrorBanner
          title={strings.couldNotLoadBranches}
          detail={messageFor(branches.error, "Try again in a moment.")}
          onRetry={() => void branches.refetch()}
        />
      ) : null}
      {changes.isError ? (
        <ErrorBanner
          title={strings.couldNotLoadIncludedChanges}
          detail={messageFor(changes.error, "Try again in a moment.")}
          onRetry={() => void changes.refetch()}
        />
      ) : (
        <ChangeTable
          rows={rows}
          loading={changes.isLoading || repositoryQuery.isPending}
          isSelected={selection.isSelected}
          onToggle={selection.toggle}
          onToggleGroup={toggleGroup}
          onToggleAll={toggleAll}
          allSelected={allSelected}
          someSelected={someSelected}
          onShowBots={() => setFilters((prev) => ({ ...prev, hideBots: false }))}
        />
      )}
      <Pagination
        page={page}
        hasNext={changes.data?.page?.hasNext ?? false}
        onChange={setPage}
        disabled={changes.isFetching}
      />
      <SelectionBar
        selected={[...selection.selected.values()]}
        building={createReview.isPending}
        onBuild={() => void build()}
        onClear={selection.clear}
        onRemove={selection.toggle}
      />
    </div>
  );
}
