import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { strings } from "@/lib/strings";

interface SelectionBarProps {
  count: number;
  building: boolean;
  onBuild: () => void;
  onClear: () => void;
}

export function SelectionBar({ count, building, onBuild, onClear }: SelectionBarProps) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-md border border-border bg-card p-3">
      <p className="text-sm text-muted-foreground">{count} selected</p>
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={onClear} disabled={count === 0 || building}>
          Clear
        </Button>
        <Button onClick={onBuild} disabled={count === 0 || building}>
          {building ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {strings.buildReview}
        </Button>
      </div>
    </div>
  );
}
