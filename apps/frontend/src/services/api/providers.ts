import { apiGet } from "@/lib/api/client";
import { unwrapList, type ListDocument } from "@/types/api/jsonapi";
import type { Provider } from "@/types/models/provider";

export const providersService = {
  async list(): Promise<Provider[]> {
    const doc = await apiGet<ListDocument<Provider>>("/api/providers");
    return unwrapList(doc);
  },
};
