import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import type { TicketGroup } from "@/lib/changes/groupByTicket";
import { strings } from "@/lib/strings";

interface TicketGroupHeaderProps {
  group: TicketGroup;
  allSelected: boolean;
  onToggleGroup: (group: TicketGroup, select: boolean) => void;
}

/** TicketGroupHeader turns "review this one feature" into a single click. */
export function TicketGroupHeader({ group, allSelected, onToggleGroup }: TicketGroupHeaderProps) {
  return (
    <TableRow className="bg-muted/50 hover:bg-muted/50">
      <TableCell colSpan={6}>
        <div className="flex items-center gap-3">
          <Badge variant={group.key === null ? "outline" : "secondary"}>{group.label}</Badge>
          <span className="text-xs text-muted-foreground">
            {strings.changesCount(group.changes.length)}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={() => onToggleGroup(group, !allSelected)}
          >
            {allSelected ? strings.deselectAll : strings.selectAllInGroup(group.changes.length)}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}
