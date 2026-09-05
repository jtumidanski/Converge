import { PatchDiff } from "@pierre/diffs/react";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

interface FileDiffProps {
  file: ReviewFileDiff;
}

/**
 * FileDiff renders one file's unified diff. @pierre/diffs parses the raw patch
 * text directly, so no full file contents are needed (FR-7.4).
 */
export function FileDiff({ file }: FileDiffProps) {
  const { binary, truncated, diff, path } = file.attributes;
  if (binary) {
    return (
      <p className="rounded-md border border-border p-4 text-sm text-muted-foreground">
        Binary file changed
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {truncated ? (
        <p className="rounded-md border border-border bg-muted p-2 text-sm text-muted-foreground">
          This file is too large to display in full. Showing the first part of the change.
        </p>
      ) : null}
      <PatchDiff
        key={path}
        patch={diff}
        options={{
          diffStyle: "unified",
          expandUnchanged: true,
          collapsedContextThreshold: 8,
          overflow: "scroll",
        }}
      />
    </div>
  );
}
