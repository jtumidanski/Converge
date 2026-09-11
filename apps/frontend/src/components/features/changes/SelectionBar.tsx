import { Loader2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Hotkey } from "@/components/common/Hotkey";
import { applyOrder } from "@/lib/changes/applyOrder";
import { strings } from "@/lib/strings";
import type { Change } from "@/types/models/change";

interface SelectionBarProps {
  selected: Change[];
  building: boolean;
  onBuild: () => void;
  onClear: () => void;
  onRemove: (change: Change) => void;
}

/**
 * SelectionBar makes the composition of a review visible before it is built:
 * the chips are numbered in apply order, which is exactly the order sent to
 * POST /api/reviews. It is sticky rather than fixed so it stays inside the
 * shared container and never overlaps the page gutters.
 */
export function SelectionBar({
  selected,
  building,
  onBuild,
  onClear,
  onRemove,
}: SelectionBarProps) {
  const ordered = applyOrder(selected);
  if (ordered.length === 0) {
    return (
      <div className="sticky bottom-4 rounded-lg border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        {strings.selectOneOrMoreChanges}
      </div>
    );
  }
  return (
    <div className="sticky bottom-4 flex flex-col gap-2 rounded-lg border border-border bg-card px-4 py-3 shadow-sm">
      <div className="flex items-center gap-2">
        <p className="text-sm text-foreground">
          {ordered.length} selected · {strings.appliedOldestToNewest}
        </p>
        <div className="ml-auto flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClear} disabled={building}>
            {strings.clear}
          </Button>
          <Button onClick={onBuild} disabled={building}>
            {building ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {strings.buildReview}
            <Hotkey className="ml-2">↵</Hotkey>
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {ordered.map((change, index) => (
          <span
            key={change.id}
            data-testid="selection-chip"
            className="flex max-w-[18rem] items-center gap-1.5 rounded-full border border-border py-1 pl-1.5 pr-1 text-xs"
          >
            <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-muted text-[0.625rem]">
              {index + 1}
            </span>
            <span className="font-mono">#{change.attributes.number}</span>
            <span className="truncate text-muted-foreground">{change.attributes.title}</span>
            <button
              type="button"
              aria-label={`Remove #${change.attributes.number}`}
              onClick={() => onRemove(change)}
              className="cursor-pointer rounded-full p-0.5 hover:bg-muted"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
      </div>
    </div>
  );
}
