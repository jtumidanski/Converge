import { Check, Copy, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { STATUS_COLOR, STATUS_LETTER } from "@/components/features/review/fileStatus";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";
import type { FileStatus } from "@/types/models/reviewFile";

interface FileHeaderProps {
  path: string;
  status: FileStatus;
  additions: number;
  deletions: number;
  href: string | undefined;
  providerName: string;
  viewed: boolean;
  onToggleViewed: () => void;
}

/** FileHeader is sticky at the top of the diff pane's own scroll container. */
export function FileHeader({
  path,
  status,
  additions,
  deletions,
  href,
  providerName,
  viewed,
  onToggleViewed,
}: FileHeaderProps) {
  const cut = path.lastIndexOf("/");
  const directory = cut === -1 ? "" : path.slice(0, cut + 1);
  const name = cut === -1 ? path : path.slice(cut + 1);

  async function copyPath(): Promise<void> {
    try {
      await navigator.clipboard.writeText(path);
      toast.success(strings.pathCopied);
    } catch {
      toast.error(strings.pathCopyFailed);
    }
  }

  return (
    <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-card px-3 py-2">
      <span className={cn("w-3 shrink-0 font-mono text-xs", STATUS_COLOR[status])}>
        {STATUS_LETTER[status]}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">
        {directory}
        <span className="font-semibold text-foreground">{name}</span>
      </span>
      <span className="shrink-0 text-xs text-foreground">+{additions}</span>
      <span className="shrink-0 text-xs text-destructive">−{deletions}</span>
      <Button variant="ghost" size="sm" onClick={() => void copyPath()}>
        <Copy className="mr-1 h-3 w-3" />
        {strings.copyPath}
      </Button>
      {href !== undefined ? (
        <Button variant="ghost" size="sm" asChild>
          <a href={href} target="_blank" rel="noreferrer">
            <ExternalLink className="mr-1 h-3 w-3" />
            {strings.openInProvider(providerName)}
          </a>
        </Button>
      ) : null}
      <Button variant={viewed ? "default" : "outline"} size="sm" onClick={onToggleViewed}>
        <Check className="mr-1 h-3 w-3" />
        {strings.viewed}
      </Button>
    </div>
  );
}
