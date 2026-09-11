import { Clock } from "lucide-react";
import { CommandEmpty, CommandGroup, CommandItem, CommandList } from "@/components/ui/command";
import { Skeleton } from "@/components/ui/skeleton";
import { relativeTime } from "@/lib/relativeTime";
import { strings } from "@/lib/strings";
import type { RecentRepository } from "@/lib/storage/recents";
import type { Repository } from "@/types/models/repository";

export interface RepositoryChoice {
  repository: string;
  defaultBranch: string;
  recent: boolean;
  openedAt?: string;
}

/**
 * mergeChoices puts matching recents first, then server results with the
 * recents removed, so the common case needs no typing and never shows a
 * repository twice (FR-13).
 */
export function mergeChoices(
  recents: RecentRepository[],
  results: Repository[],
  query: string,
): RepositoryChoice[] {
  const needle = query.trim().toLowerCase();
  const matching = recents.filter(
    (r) => needle === "" || r.repository.toLowerCase().includes(needle),
  );
  const seen = new Set(matching.map((r) => r.repository));
  const fromServer = results
    .filter((r) => !seen.has(r.id))
    .map((r) => ({
      repository: r.id,
      defaultBranch: r.attributes.defaultBranch,
      recent: false,
    }));
  return [
    ...matching.map((r) => ({
      repository: r.repository,
      defaultBranch: r.defaultBranch,
      recent: true,
      openedAt: r.openedAt,
    })),
    ...fromServer,
  ];
}

interface RepositoryResultsProps {
  choices: RepositoryChoice[];
  selected: string | undefined;
  loading: boolean;
  onSelect: (choice: RepositoryChoice) => void;
}

export function RepositoryResults({
  choices,
  selected,
  loading,
  onSelect,
}: RepositoryResultsProps) {
  return (
    <CommandList className="max-h-[22rem]">
      {loading ? (
        <div className="space-y-2 p-2">
          {[0, 1, 2].map((row) => (
            <Skeleton key={row} className="h-9 w-full" />
          ))}
        </div>
      ) : (
        <>
          <CommandEmpty>{strings.noRepositoriesMatchSearch}</CommandEmpty>
          <CommandGroup>
            {choices.map((choice) => (
              <CommandItem
                key={choice.repository}
                value={choice.repository}
                onSelect={() => onSelect(choice)}
                data-selected-repository={choice.repository === selected ? "" : undefined}
                className={choice.repository === selected ? "bg-accent" : undefined}
              >
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-medium">{choice.repository}</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {choice.defaultBranch}
                  </span>
                </span>
                {choice.recent && choice.openedAt ? (
                  <span className="ml-2 flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
                    <Clock className="h-3 w-3" />
                    opened {relativeTime(choice.openedAt)}
                  </span>
                ) : null}
              </CommandItem>
            ))}
          </CommandGroup>
        </>
      )}
    </CommandList>
  );
}
