import type { FileStatus } from "@/types/models/reviewFile";

/**
 * The tree row and the diff header render the same status marker, so the letter
 * and its colour live here once rather than drifting apart in two components.
 */
export const STATUS_LETTER: Record<FileStatus, string> = {
  modified: "M",
  added: "A",
  deleted: "D",
  renamed: "R",
};

export const STATUS_COLOR: Record<FileStatus, string> = {
  modified: "text-warning",
  added: "text-success",
  deleted: "text-destructive",
  renamed: "text-info",
};
