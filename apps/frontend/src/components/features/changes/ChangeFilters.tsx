import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { ChangeSearch } from "@/components/features/changes/ChangeSearch";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";

interface ChangeFiltersProps {
  search: string;
  onSearchChange: (value: string) => void;
  /** Derived from the loaded pages only; loading another page may add chips. */
  authors: string[];
  activeAuthors: ReadonlySet<string>;
  onToggleAuthor: (author: string) => void;
  hideBots: boolean;
  onHideBotsChange: (next: boolean) => void;
  groupByTicket: boolean;
  onGroupByTicketChange: (next: boolean) => void;
  shown: number;
  total: number;
}

/** ChangeFilters is the search + author chips + toggles row above the change table. */
export function ChangeFilters({
  search,
  onSearchChange,
  authors,
  activeAuthors,
  onToggleAuthor,
  hideBots,
  onHideBotsChange,
  groupByTicket,
  onGroupByTicketChange,
  shown,
  total,
}: ChangeFiltersProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <ChangeSearch value={search} onChange={onSearchChange} />
      {authors.map((author) => {
        const active = activeAuthors.has(author);
        return (
          <button
            key={author}
            type="button"
            aria-pressed={active}
            onClick={() => onToggleAuthor(author)}
            className={cn(
              "cursor-pointer rounded-full border px-2.5 py-1 text-xs",
              active
                ? "border-foreground bg-foreground text-background"
                : "border-border text-muted-foreground hover:text-foreground",
            )}
          >
            {author}
          </button>
        );
      })}
      <Separator orientation="vertical" className="h-5" />
      <label className="flex items-center gap-2 rounded-full border border-border px-2.5 py-1 text-xs text-muted-foreground">
        <Switch
          checked={hideBots}
          onCheckedChange={onHideBotsChange}
          aria-label={strings.hideDependencyBots}
        />
        {strings.hideDependencyBots}
      </label>
      <Button
        type="button"
        variant={groupByTicket ? "default" : "outline"}
        size="sm"
        className="rounded-full"
        aria-pressed={groupByTicket}
        onClick={() => onGroupByTicketChange(!groupByTicket)}
      >
        {strings.groupByTicket}
      </Button>
      <span className="ml-auto text-xs text-muted-foreground">
        {strings.shownOfTotal(shown, total)}
      </span>
    </div>
  );
}
