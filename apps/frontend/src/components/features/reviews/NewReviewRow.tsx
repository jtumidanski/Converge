import { Plus } from "lucide-react";
import { Hotkey } from "@/components/common/Hotkey";
import { TableCell, TableRow } from "@/components/ui/table";
import { strings } from "@/lib/strings";

/** NewReviewRow is the dashed last row that opens the new-review drawer. */
export function NewReviewRow({ onClick }: { onClick: () => void }) {
  return (
    <TableRow className="border-dashed hover:bg-transparent">
      <TableCell colSpan={5} className="p-0">
        <button
          type="button"
          onClick={onClick}
          className="flex w-full cursor-pointer items-center gap-2 px-4 py-3 text-sm text-muted-foreground hover:text-foreground"
        >
          <Plus className="h-4 w-4" />
          {strings.startNewReviewRow}
          <Hotkey className="ml-auto">n</Hotkey>
        </button>
      </TableCell>
    </TableRow>
  );
}
