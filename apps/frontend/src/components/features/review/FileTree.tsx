import { useMemo } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { ReviewFile } from "@/types/models/reviewFile";

interface FileTreeProps {
  files: ReviewFile[];
  selectedPath: string | undefined;
  onSelect: (path: string) => void;
}

function directoryOf(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? "/" : path.slice(0, index);
}

function baseNameOf(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

export function FileTree({ files, selectedPath, onSelect }: FileTreeProps) {
  const grouped = useMemo(() => {
    const map = new Map<string, ReviewFile[]>();
    for (const file of files) {
      const dir = directoryOf(file.attributes.path);
      const bucket = map.get(dir);
      if (bucket) {
        bucket.push(file);
      } else {
        map.set(dir, [file]);
      }
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [files]);

  return (
    <nav className="flex flex-col gap-3" aria-label="Changed files">
      {grouped.map(([directory, entries]) => (
        <div key={directory}>
          <p className="px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {directory}
          </p>
          <ul>
            {entries.map((file) => {
              const { path, status, additions, deletions } = file.attributes;
              const active = path === selectedPath;
              return (
                <li key={path}>
                  <button
                    type="button"
                    aria-current={active ? "true" : undefined}
                    onClick={() => onSelect(path)}
                    className={cn(
                      "flex w-full cursor-pointer items-center justify-between gap-2 rounded px-2 py-1 text-left text-sm",
                      active ? "bg-accent text-accent-foreground" : "hover:bg-muted",
                    )}
                  >
                    <span className="truncate font-mono">{baseNameOf(path)}</span>
                    <span className="flex shrink-0 items-center gap-1">
                      <Badge variant="outline" className="capitalize">
                        {status}
                      </Badge>
                      <span className="text-xs text-muted-foreground">+{additions}</span>
                      <span className="text-xs text-destructive">−{deletions}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </nav>
  );
}
