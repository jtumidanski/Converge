import { apiGet } from "@/lib/api/client";
import { buildQuery } from "@/services/api/repositories";
import { unwrapList, type ListDocument, type PageMeta } from "@/types/api/jsonapi";
import type { Change } from "@/types/models/change";

export interface ChangeListParams {
  target?: string;
  search?: string;
  page?: number;
  pageSize?: number;
}

export interface PagedChanges {
  items: Change[];
  page?: PageMeta;
}

export const changesService = {
  async list(
    providerId: string,
    repository: string,
    params: ChangeListParams = {},
  ): Promise<PagedChanges> {
    const path = `/api/providers/${encodeURIComponent(providerId)}/repositories/${encodeURIComponent(repository)}/changes`;
    const doc = await apiGet<ListDocument<Change>>(
      `${path}${buildQuery({ state: "merged", target: params.target, search: params.search, page: params.page, pageSize: params.pageSize })}`,
    );
    const items = unwrapList(doc);
    return doc.meta?.page ? { items, page: doc.meta.page } : { items };
  },
};
