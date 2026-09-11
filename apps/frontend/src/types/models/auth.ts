export type AuthModeName = "standalone" | "hosted";

export interface AuthMode {
  mode: AuthModeName;
  registrationOpen: boolean;
}

export interface CurrentUser {
  id: string;
  username: string;
  createdAt: string;
  providerCount: number;
}

export type ProviderKind = "github" | "gitlab";

export interface UserProvider {
  id: string;
  slug: string;
  displayName: string;
  kind: ProviderKind;
  baseUrl: string;
  tokenLast4: string;
  tokenSetAt: string;
  createdAt: string;
  updatedAt: string;
}
