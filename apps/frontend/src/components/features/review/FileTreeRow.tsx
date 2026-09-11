import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import { strings } from "@/lib/strings";
import type { FileStatus, ReviewFile } from "@/types/models/reviewFile";

const STATUS_LETTER: Record<FileStatus, string> = {
  modified: "M",
  added: "A",
  deleted: "D",
  renamed: "R",
};

const STATUS_COLOR: Record<FileStatus, string> = {
  modified: "text-amber-500",
  added: "text-green-500",
  deleted: "text-destructive",
  renamed: "text-blue-500",
};

function baseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

interface FileTreeRowProps {
  file: ReviewFile;
  depth: number;
  selected: boolean;
  viewed: boolean;
  onSelect: (path: string, source: "pointer" | "keyboard") => void;
  onToggleViewed: (path: string) => void;
}

export function FileTreeRow({
  file,
  depth,
  selected,
  viewed,
  onSelect,
  onToggleViewed,
}: FileTreeRowProps) {
  const { path, status, additions, deletions } = file.attributes;
  const name = baseName(path);
  return (
    <div
      role="treeitem"
      aria-selected={selected}
      aria-label={path}
      data-path={path}
      onClick={() => onSelect(path, "pointer")}
      className={cn(
        "flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm",
        // A filled background and brighter, bolder text mark the selection --
        // no left accent bar, per the agreed mockup.
        selected ? "bg-accent font-medium text-accent-foreground" : "hover:bg-muted",
        viewed && !selected && "text-muted-foreground opacity-70",
      )}
      style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}
    >
      <span onClick={(event) => event.stopPropagation()}>
        <Checkbox
          checked={viewed}
          aria-label={strings.markFileViewed(name)}
          onCheckedChange={() => onToggleViewed(path)}
        />
      </span>
      <span className={cn("w-3 shrink-0 font-mono text-xs", STATUS_COLOR[status])}>
        {STATUS_LETTER[status]}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs">{name}</span>
      <span className="shrink-0 text-[0.6875rem] text-foreground">+{additions}</span>
      <span className="shrink-0 text-[0.6875rem] text-destructive">−{deletions}</span>
    </div>
  );
}
