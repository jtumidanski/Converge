import type { ReviewFile } from "@/types/models/reviewFile";

export interface ViewedProgress {
  viewed: number;
  total: number;
  /** Rounded whole percent; 0 when there are no files (never NaN). */
  percent: number;
}

/**
 * viewedProgress counts only paths that are still in the file list, so a stale
 * localStorage entry for a file that no longer appears cannot push the counter
 * past the total.
 */
export function viewedProgress(files: ReviewFile[], viewed: ReadonlySet<string>): ViewedProgress {
  const total = files.length;
  let count = 0;
  for (const file of files) {
    if (viewed.has(file.attributes.path)) count += 1;
  }
  return { viewed: count, total, percent: total === 0 ? 0 : Math.round((count / total) * 100) };
}
