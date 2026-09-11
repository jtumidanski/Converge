import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { FileTreeRow } from "@/components/features/review/FileTreeRow";
import { ancestorDirs, buildTree, filterTree, type TreeNode } from "@/lib/review/fileTree";
import { strings } from "@/lib/strings";
import type { ReviewFile } from "@/types/models/reviewFile";

interface FileTreeProps {
  files: ReviewFile[];
  viewed: ReadonlySet<string>;
  selectedPath: string | undefined;
  onSelect: (path: string, source: "pointer" | "keyboard") => void;
  onToggleViewed: (path: string) => void;
  /** Set when the selection came from j/k or the footer, so the row scrolls into view. */
  scrollSelectionIntoView?: boolean;
}

export function FileTree({
  files,
  viewed,
  selectedPath,
  onSelect,
  onToggleViewed,
  scrollSelectionIntoView = false,
}: FileTreeProps) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [query, setQuery] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);

  // Memoised per file list and per query: a 200-file tree is rebuilt on every
  // keystroke otherwise, and this component re-renders on every viewed toggle.
  const tree = useMemo(() => buildTree(files), [files]);
  const filtered = useMemo(() => filterTree(tree, query), [tree, query]);
  const filtering = query.trim() !== "";

  // The selected file must always be reachable: expanding its ancestors is
  // what makes j/k across a collapsed directory land somewhere visible.
  // Adjusted during render rather than in an effect, per
  // https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes
  // -- the same pattern useSelection.ts already uses in this codebase.
  const [prevSelectedPath, setPrevSelectedPath] = useState(selectedPath);
  if (selectedPath !== prevSelectedPath) {
    setPrevSelectedPath(selectedPath);
    if (selectedPath !== undefined) {
      const ancestors = ancestorDirs(selectedPath);
      if (ancestors.some((dir) => collapsed.has(dir))) {
        const next = new Set(collapsed);
        for (const dir of ancestors) next.delete(dir);
        setCollapsed(next);
      }
    }
  }

  useEffect(() => {
    if (!scrollSelectionIntoView || selectedPath === undefined) return;
    const row = containerRef.current?.querySelector(`[data-path="${CSS.escape(selectedPath)}"]`);
    row?.scrollIntoView({ block: "nearest" });
  }, [scrollSelectionIntoView, selectedPath]);

  function toggleDir(path: string): void {
    setCollapsed((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  function renderNodes(nodes: TreeNode[], depth: number): React.ReactNode {
    return nodes.map((node) => {
      if (node.kind === "file") {
        return (
          <FileTreeRow
            key={node.file.attributes.path}
            file={node.file}
            depth={depth}
            selected={node.file.attributes.path === selectedPath}
            viewed={viewed.has(node.file.attributes.path)}
            onSelect={onSelect}
            onToggleViewed={onToggleViewed}
          />
        );
      }
      // A filter result is always open: hiding a match behind a collapsed
      // parent would make the filter look broken.
      const open = filtering || !collapsed.has(node.path);
      return (
        <div key={node.path}>
          <button
            type="button"
            onClick={() => toggleDir(node.path)}
            aria-label={
              open ? strings.collapseDirectory(node.path) : strings.expandDirectory(node.path)
            }
            aria-expanded={open}
            className="flex w-full cursor-pointer items-center gap-1 rounded px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted"
            style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}
          >
            {open ? (
              <ChevronDown className="h-3 w-3 shrink-0" />
            ) : (
              <ChevronRight className="h-3 w-3 shrink-0" />
            )}
            {/* The full path, not just node.name: a collapsed chain like
                main/java/com/atlas still needs its "src" ancestor visible,
                and node.path is the only field that always carries it. */}
            <span className="truncate font-mono">{node.path}</span>
          </button>
          {open ? renderNodes(node.children, depth + 1) : null}
        </div>
      );
    });
  }

  return (
    <div className="flex h-full min-h-0 flex-col border-r border-border">
      <div className="border-b border-border p-2">
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={strings.filterFilesPlaceholder}
          className="h-8 text-xs"
        />
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div ref={containerRef} role="tree" aria-label={strings.changedFiles} className="p-1">
          {renderNodes(filtered, 0)}
        </div>
      </ScrollArea>
    </div>
  );
}
