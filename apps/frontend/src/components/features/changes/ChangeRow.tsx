import type { MouseEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { TableCell, TableRow } from "@/components/ui/table";
import { ticketKey } from "@/lib/changes/ticketKey";
import { strings } from "@/lib/strings";
import type { Change } from "@/types/models/change";

const TWO_DAYS_MS = 172_800_000;

/** mergedLabel shows the time only while it is still useful for ordering. */
function mergedLabel(value: string | null, now: Date = new Date()): string {
  if (value === null) return "—";
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) return "—";
  const date = new Date(ms);
  return now.getTime() - ms < TWO_DAYS_MS
    ? date.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
    : date.toLocaleDateString();
}

function initials(author: string): string {
  return author.slice(0, 2).toUpperCase();
}

interface ChangeRowProps {
  change: Change;
  selected: boolean;
  showTicketBadge: boolean;
  onToggle: (change: Change) => void;
}

export function ChangeRow({ change, selected, showTicketBadge, onToggle }: ChangeRowProps) {
  const { number, title, author, mergedAt, sourceBranch, webUrl } = change.attributes;
  const key = showTicketBadge ? ticketKey(title) : null;

  // Clicking anywhere selects, except on the link or the checkbox itself: the
  // provider link has to stay usable without also toggling the row underneath
  // it, and the checkbox's own onCheckedChange already toggles — without this
  // exclusion a checkbox click bubbles to the row and double-toggles back to
  // the original state.
  function onRowClick(event: MouseEvent<HTMLTableRowElement>): void {
    if ((event.target as HTMLElement).closest("a, button")) return;
    onToggle(change);
  }

  return (
    <TableRow
      onClick={onRowClick}
      className="cursor-pointer"
      data-state={selected ? "selected" : undefined}
    >
      <TableCell className="w-10">
        <Checkbox
          checked={selected}
          onCheckedChange={() => onToggle(change)}
          aria-label={strings.selectHash(number)}
        />
      </TableCell>
      <TableCell className="w-20 font-mono text-sm">
        <a
          href={webUrl}
          target="_blank"
          rel="noreferrer"
          className="text-muted-foreground hover:text-foreground hover:underline"
        >
          #{number}
        </a>
      </TableCell>
      <TableCell className="font-medium">
        <span className="flex items-center gap-2">
          {key ? <Badge variant="secondary">{key}</Badge> : null}
          <span className="truncate">{title}</span>
        </span>
      </TableCell>
      <TableCell className="w-44 text-muted-foreground">
        <span className="flex items-center gap-2">
          <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-muted text-[0.625rem] font-medium">
            {initials(author)}
          </span>
          <span className="truncate">{author}</span>
        </span>
      </TableCell>
      <TableCell className="w-36 text-xs text-muted-foreground">{mergedLabel(mergedAt)}</TableCell>
      <TableCell className="w-48 truncate font-mono text-xs text-muted-foreground">
        {sourceBranch}
      </TableCell>
    </TableRow>
  );
}
