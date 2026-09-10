import type { Resource } from "@/types/api/jsonapi";

export type FileStatus = "added" | "modified" | "deleted" | "renamed";

export interface ReviewFileAttributes {
  path: string;
  // The backend's reviewFileAttributes.PreviousPath is a plain (non-pointer)
  // Go string field with no `omitempty`, so it always marshals as a string
  // (empty when there is no rename) and never as JSON null. The brief typed
  // this "string | null"; this deviates to match the verified Go contract
  // (apps/backend/internal/api/review_files.go).
  previousPath: string;
  status: FileStatus;
  additions: number;
  deletions: number;
  binary: boolean;
}

export type ReviewFile = Resource<"review-files", ReviewFileAttributes>;

export interface ReviewFileDiffAttributes extends ReviewFileAttributes {
  truncated: boolean;
  diff: string;
}

export type ReviewFileDiff = Resource<"review-file-diffs", ReviewFileDiffAttributes>;
