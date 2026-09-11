import { apiGet } from "@/lib/api/client";
import { buildQuery } from "@/services/api/repositories";
import { unwrapList, type ListDocument, type PageMeta } from "@/types/api/jsonapi";
import type { Branch } from "@/types/models/branch";

export interface PagedBranches {
  items: Branch[];
  page?: PageMeta;
}

export interface BranchListParams {
  search?: string;
  page?: number;
  pageSize?: number;
}

export const branchesService = {
  async list(
    providerId: string,
    repository: string,
    params: BranchListParams = {},
    init: RequestInit = {},
  ): Promise<PagedBranches> {
    const query = buildQuery({
      search: params.search,
      page: params.page,
      pageSize: params.pageSize,
    });
    const doc = await apiGet<ListDocument<Branch>>(
      `/api/providers/${encodeURIComponent(providerId)}/repositories/${encodeURIComponent(repository)}/branches${query}`,
      init,
    );
    const items = unwrapList(doc);
    return doc.meta?.page ? { items, page: doc.meta.page } : { items };
  },
};
