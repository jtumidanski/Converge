import type { Resource } from "@/types/api/jsonapi";

export type ProviderKind = "github" | "gitlab";

export interface ProviderAttributes {
  displayName: string;
  kind: ProviderKind;
  baseUrl: string;
}

export type Provider = Resource<"providers", ProviderAttributes>;
