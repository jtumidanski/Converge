import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Hotkey } from "@/components/common/Hotkey";
import { strings } from "@/lib/strings";

interface FileFooterProps {
  index: number;
  total: number;
  nextName: string | undefined;
  isLast: boolean;
  onNext: () => void;
}

/** FileFooter gives the reviewer forward motion without a trip to the tree. */
export function FileFooter({ index, total, nextName, isLast, onNext }: FileFooterProps) {
  return (
    <div className="flex items-center gap-2 border-t border-border px-3 py-2">
      <span className="text-xs text-muted-foreground">{strings.fileOfTotal(index + 1, total)}</span>
      <Button variant="outline" size="sm" className="ml-auto" onClick={onNext}>
        {isLast ? strings.backToFirstFile : `${strings.nextFile}: ${nextName ?? ""}`}
        <ArrowRight className="ml-1 h-3 w-3" />
        <Hotkey className="ml-2">j</Hotkey>
      </Button>
    </div>
  );
}
