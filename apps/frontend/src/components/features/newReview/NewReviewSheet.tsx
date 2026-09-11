import { useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Command } from "@/components/ui/command";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { ProviderSelect } from "@/components/features/newReview/ProviderSelect";
import { RepositoryResults, mergeChoices } from "@/components/features/newReview/RepositoryResults";
import { RepositorySearch } from "@/components/features/newReview/RepositorySearch";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { repositoryKeys, useRepositories } from "@/lib/hooks/api/useRepositories";
import { useDebouncedValue } from "@/lib/hooks/useDebouncedValue";
import { mostRecentProvider, recentsFor } from "@/lib/storage/recents";
import { parseRepositoryInput } from "@/lib/repositoryInput";
import { repositoriesService } from "@/services/api";
import { ApiError, messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

const MIN_SEARCH = 2;

interface NewReviewSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * NewReviewSheet is the one entry point for starting a review. It writes no
 * recent entry itself: the create page records one on load (FR-16) so a deep
 * link counts the same as a trip through this drawer.
 */
export function NewReviewSheet({ open, onOpenChange }: NewReviewSheetProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const providers = useProviders();
  const [chosenProvider, setChosenProvider] = useState<string | undefined>(undefined);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const [inlineError, setInlineError] = useState<string | null>(null);
  const [resolving, setResolving] = useState(false);

  const list = providers.data ?? [];
  const preferred = mostRecentProvider();
  const providerId =
    chosenProvider ??
    (preferred !== undefined && list.some((p) => p.id === preferred) ? preferred : list[0]?.id);
  const provider = list.find((p) => p.id === providerId);

  const debounced = useDebouncedValue(query, 250);
  const activeSearch = debounced.trim().length >= MIN_SEARCH ? debounced.trim() : undefined;
  const repositories = useRepositories(
    providerId,
    activeSearch !== undefined ? { search: activeSearch } : {},
  );

  const choices = useMemo(
    () =>
      mergeChoices(
        providerId === undefined ? [] : recentsFor(providerId),
        repositories.data?.items ?? [],
        query,
      ),
    [providerId, repositories.data, query],
  );

  function reset(): void {
    setChosenProvider(undefined);
    setQuery("");
    setSelected(undefined);
    setInlineError(null);
  }

  function close(next: boolean): void {
    if (!next) reset();
    onOpenChange(next);
  }

  function go(repository: string): void {
    const search = new URLSearchParams({ provider: providerId as string, repo: repository });
    close(false);
    navigate(`/select?${search.toString()}`);
  }

  // Enter with nothing highlighted means "resolve what I pasted". cmdk marks
  // the event handled when it selects a row, so this only runs otherwise.
  async function resolveTyped(): Promise<void> {
    if (providerId === undefined) return;
    const fullName = parseRepositoryInput(query, provider?.attributes.baseUrl);
    if (fullName === null) return;
    setInlineError(null);
    setResolving(true);
    try {
      await queryClient.fetchQuery({
        queryKey: repositoryKeys.detail(providerId, fullName),
        queryFn: () => repositoriesService.get(providerId, fullName),
      });
      go(fullName);
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 404) {
        setInlineError(strings.repositoryNotFound(fullName));
      } else {
        toast.error(messageFor(error, strings.repositoryCheckFailed));
      }
    } finally {
      setResolving(false);
    }
  }

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent side="right" className="flex w-full flex-col sm:max-w-[32.5rem]">
        <SheetHeader>
          <SheetTitle>{strings.newReview}</SheetTitle>
          <SheetDescription>{strings.pickProviderAndRepository}</SheetDescription>
        </SheetHeader>
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4">
          <div className="flex flex-col gap-1">
            <label htmlFor="provider-select" className="text-sm font-medium text-foreground">
              {strings.provider}
            </label>
            <ProviderSelect
              providers={list}
              value={providerId}
              loading={providers.isLoading}
              onChange={(id) => {
                setChosenProvider(id);
                setSelected(undefined);
                setInlineError(null);
              }}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-foreground">{strings.repository}</span>
            {/* shouldFilter=false: the server already filtered, and cmdk's own
                fuzzy filter would hide server results that do not fuzzy-match. */}
            <Command shouldFilter={false} className="rounded-md border border-border">
              <RepositorySearch
                value={query}
                onChange={(value) => {
                  setQuery(value);
                  setInlineError(null);
                }}
                onEnter={() => void resolveTyped()}
              />
              <RepositoryResults
                choices={choices}
                selected={selected}
                loading={repositories.isLoading || resolving}
                onSelect={(choice) => setSelected(choice.repository)}
              />
            </Command>
            {inlineError ? <p className="text-sm text-destructive">{inlineError}</p> : null}
          </div>
        </div>
        <SheetFooter className="flex-row justify-end gap-2">
          <Button variant="ghost" onClick={() => close(false)}>
            {strings.cancel}
          </Button>
          <Button disabled={selected === undefined} onClick={() => selected && go(selected)}>
            {strings.chooseChanges} →
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
