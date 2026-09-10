import type { Resource } from "@/types/api/jsonapi";

export interface ChangeAttributes {
  number: number;
  title: string;
  author: string;
  sourceBranch: string;
  targetBranch: string;
  mergedAt: string | null;
  createdAt: string | null;
  landingSha: string | null;
  webUrl: string;
}

export type Change = Resource<"changes", ChangeAttributes>;

/** shortSha renders a landing SHA for the table, or an em dash when unknown. */
export function shortSha(sha: string | null): string {
  return sha ? sha.slice(0, 7) : "—";
}
