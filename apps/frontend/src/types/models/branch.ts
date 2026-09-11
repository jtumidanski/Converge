import type { Resource } from "@/types/api/jsonapi";

export interface BranchAttributes {
  name: string;
  isDefault: boolean;
  /** The branch tip. Empty when the provider did not report one. */
  sha: string;
}

export type Branch = Resource<"branches", BranchAttributes>;
