import { apiDelete, apiGet, apiPatch, apiPost } from "@/lib/api/client";
import {
  unwrapList,
  unwrapOne,
  type Document,
  type ListDocument,
  type Resource,
} from "@/types/api/jsonapi";
import type { ProviderKind, UserProvider } from "@/types/models/auth";

type UserProviderResource = Resource<"userProviders", Omit<UserProvider, "id">>;

function toUserProvider(r: UserProviderResource): UserProvider {
  return { id: r.id, ...r.attributes };
}

export interface UserProviderCreate {
  slug: string;
  displayName: string;
  kind: ProviderKind;
  baseUrl: string;
  token: string;
  validate: boolean;
}

export interface UserProviderPatch {
  displayName?: string;
  kind?: ProviderKind;
  baseUrl?: string;
  token?: string;
  validate?: boolean;
}

export const userProvidersService = {
  async list(): Promise<UserProvider[]> {
    const doc = await apiGet<ListDocument<UserProviderResource>>("/api/settings/providers");
    return unwrapList(doc).map(toUserProvider);
  },
  async create(input: UserProviderCreate): Promise<UserProvider> {
    const doc = await apiPost<Document<UserProviderResource>>("/api/settings/providers", {
      data: { type: "userProviders", attributes: { ...input } },
    });
    return toUserProvider(unwrapOne(doc));
  },
  async update(id: string, patch: UserProviderPatch): Promise<UserProvider> {
    // An omitted token leaves the stored one unchanged (FR-5.5); an explicit
    // empty string means the same thing, so it is dropped entirely rather
    // than sent as "".
    const { token, ...rest } = patch;
    const attributes = token ? { ...rest, token } : rest;
    const doc = await apiPatch<Document<UserProviderResource>>(`/api/settings/providers/${id}`, {
      data: { type: "userProviders", id, attributes },
    });
    return toUserProvider(unwrapOne(doc));
  },
  async remove(id: string): Promise<void> {
    await apiDelete(`/api/settings/providers/${id}`);
  },
};
