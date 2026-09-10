import { apiDelete, apiGet, apiGetText, apiPost } from "@/lib/api/client";
import { unwrapList, unwrapOne, type Document, type ListDocument } from "@/types/api/jsonapi";
import type { CreateReviewRequest, Review } from "@/types/models/review";
import type { ReviewFile, ReviewFileDiff } from "@/types/models/reviewFile";

export const reviewsService = {
  async create(request: CreateReviewRequest): Promise<Review> {
    const doc = await apiPost<Document<Review>>("/api/reviews", {
      data: { type: "reviews", attributes: request },
    });
    return unwrapOne(doc);
  },

  async get(id: string): Promise<Review> {
    const doc = await apiGet<Document<Review>>(`/api/reviews/${encodeURIComponent(id)}`);
    return unwrapOne(doc);
  },

  async list(): Promise<Review[]> {
    const doc = await apiGet<ListDocument<Review>>("/api/reviews");
    return unwrapList(doc);
  },

  async files(id: string): Promise<ReviewFile[]> {
    const doc = await apiGet<ListDocument<ReviewFile>>(
      `/api/reviews/${encodeURIComponent(id)}/files`,
    );
    return unwrapList(doc);
  },

  async fileDiff(id: string, path: string): Promise<ReviewFileDiff> {
    const doc = await apiGet<Document<ReviewFileDiff>>(
      `/api/reviews/${encodeURIComponent(id)}/files/${path.split("/").map(encodeURIComponent).join("/")}`,
    );
    return unwrapOne(doc);
  },

  async combinedDiff(id: string): Promise<string> {
    return apiGetText(`/api/reviews/${encodeURIComponent(id)}/diff`);
  },

  async remove(id: string): Promise<void> {
    await apiDelete(`/api/reviews/${encodeURIComponent(id)}`);
  },
};
