import type { Resource } from "@/types/api/jsonapi";

export type ReviewStatus = "CREATING" | "READY" | "CONFLICTED" | "FAILED" | "FINISHED" | "EXPIRED";

export interface ReviewDiagnostics {
  workspacePath?: string;
  branch?: string;
  strategy?: string;
  sourceSha?: string;
}

export interface ReviewErrorPayload {
  code: string;
  message: string;
  change?: number;
  commit?: string;
  conflictingFiles?: string[];
  appliedChanges?: number[];
  possibleDependency?: boolean;
  diagnostics?: ReviewDiagnostics;
}

export interface IncludedChange {
  number: number;
  title: string;
  author: string;
  mergedAt: string | null;
  webUrl: string;
  strategy: string;
}

export interface Totals {
  files: number;
  additions: number;
  deletions: number;
}

export interface ReviewAttributes {
  status: ReviewStatus;
  stage: string | null;
  provider: string;
  repository: string;
  baseBranch: string;
  baseSha: string | null;
  headSha: string | null;
  baseDescription: string;
  changes: number[];
  included: IncludedChange[];
  totals: Totals | null;
  error: ReviewErrorPayload | null;
  createdAt: string;
  updatedAt: string;
  expiresAt: string;
}

export type Review = Resource<"reviews", ReviewAttributes>;

export interface CreateReviewRequest {
  provider: string;
  repository: string;
  baseBranch?: string;
  changes: number[];
}

/** isTerminal reports whether status is a settled state (not still building). */
export function isTerminal(status: ReviewStatus): boolean {
  return status !== "CREATING";
}
