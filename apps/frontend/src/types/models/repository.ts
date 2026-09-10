import type { Resource } from "@/types/api/jsonapi";

export interface RepositoryAttributes {
  name: string;
  namespace: string;
  defaultBranch: string;
  webUrl: string;
}

export type Repository = Resource<"repositories", RepositoryAttributes>;
