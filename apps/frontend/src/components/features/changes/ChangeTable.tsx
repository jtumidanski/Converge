import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/common/EmptyState";
import { ChangeRow } from "@/components/features/changes/ChangeRow";
import { TicketGroupHeader } from "@/components/features/changes/TicketGroupHeader";
import type { TicketGroup } from "@/lib/changes/groupByTicket";
import { strings } from "@/lib/strings";
import type { Change } from "@/types/models/change";

export type ChangeTableRow =
  | { kind: "group"; group: TicketGroup; allSelected: boolean }
  | { kind: "change"; change: Change; showTicketBadge: boolean }
  | { kind: "hidden"; count: number };

/**
 * buildRows flattens grouped and ungrouped modes into one row list, so the
 * table markup is the same in both. Pass groups=null for the flat table, where
 * each change carries its own ticket badge instead.
 */
export function buildRows(
  visible: Change[],
  groups: TicketGroup[] | null,
  hiddenBots: number,
  isSelected: (n: number) => boolean,
): ChangeTableRow[] {
  const rows: ChangeTableRow[] = [];
  if (groups === null) {
    for (const change of visible) rows.push({ kind: "change", change, showTicketBadge: true });
  } else {
    for (const group of groups) {
      rows.push({
        kind: "group",
        group,
        allSelected:
          group.changes.length > 0 && group.changes.every((c) => isSelected(c.attributes.number)),
      });
      for (const change of group.changes) {
        rows.push({ kind: "change", change, showTicketBadge: false });
      }
    }
  }
  if (hiddenBots > 0) rows.push({ kind: "hidden", count: hiddenBots });
  return rows;
}

interface ChangeTableProps {
  rows: ChangeTableRow[];
  loading: boolean;
  isSelected: (n: number) => boolean;
  onToggle: (change: Change) => void;
  onToggleGroup: (group: TicketGroup, select: boolean) => void;
  onToggleAll: (select: boolean) => void;
  allSelected: boolean;
  someSelected: boolean;
  onShowBots: () => void;
}

export function ChangeTable({
  rows,
  loading,
  isSelected,
  onToggle,
  onToggleGroup,
  onToggleAll,
  allSelected,
  someSelected,
  onShowBots,
}: ChangeTableProps) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3, 4].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }
  if (rows.length === 0) {
    return (
      <EmptyState
        title={strings.noMergedChanges}
        description={strings.noMergedChangesDescription}
      />
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10">
            <Checkbox
              aria-label={strings.selectAllVisibleChanges}
              checked={allSelected ? true : someSelected ? "indeterminate" : false}
              onCheckedChange={() => onToggleAll(!allSelected)}
            />
          </TableHead>
          <TableHead className="w-20">{strings.columnNumber}</TableHead>
          <TableHead>{strings.columnTitle}</TableHead>
          <TableHead className="w-44">{strings.columnAuthor}</TableHead>
          <TableHead className="w-36">{strings.columnMerged}</TableHead>
          <TableHead className="w-48">{strings.columnSourceBranch}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => {
          if (row.kind === "group") {
            return (
              <TicketGroupHeader
                key={`group-${row.group.label}`}
                group={row.group}
                allSelected={row.allSelected}
                onToggleGroup={onToggleGroup}
              />
            );
          }
          if (row.kind === "hidden") {
            return (
              <TableRow key="hidden-bots" className="hover:bg-transparent">
                <TableCell colSpan={6} className="text-xs text-muted-foreground">
                  {strings.changesHiddenBy(row.count)}
                  <Button variant="ghost" size="sm" className="ml-2" onClick={onShowBots}>
                    {strings.show}
                  </Button>
                </TableCell>
              </TableRow>
            );
          }
          return (
            <ChangeRow
              key={row.change.id}
              change={row.change}
              selected={isSelected(row.change.attributes.number)}
              showTicketBadge={row.showTicketBadge}
              onToggle={onToggle}
            />
          );
        })}
      </TableBody>
    </Table>
  );
}
