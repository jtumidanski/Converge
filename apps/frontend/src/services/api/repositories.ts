import { apiGet } from "@/lib/api/client";
import {
  unwrapList,
  unwrapOne,
  type Document,
  type ListDocument,
  type PageMeta,
} from "@/types/api/jsonapi";
import type { Repository } from "@/types/models/repository";

export interface PagedRepositories {
  items: Repository[];
  page?: PageMeta;
}

export interface RepositoryListParams {
  page?: number;
  pageSize?: number;
}

/** Builds a query string from params, dropping undefined and empty values. */
function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export const repositoriesService = {
  async list(providerId: string, params: RepositoryListParams = {}): Promise<PagedRepositories> {
    const doc = await apiGet<ListDocument<Repository>>(
      `/api/providers/${encodeURIComponent(providerId)}/repositories${query({ page: params.page, pageSize: params.pageSize })}`,
    );
    const items = unwrapList(doc);
    return doc.meta?.page ? { items, page: doc.meta.page } : { items };
  },

  async get(providerId: string, fullName: string): Promise<Repository> {
    const doc = await apiGet<Document<Repository>>(
      `/api/providers/${encodeURIComponent(providerId)}/repositories/${encodeURIComponent(fullName)}`,
    );
    return unwrapOne(doc);
  },
};

export { query as buildQuery };
