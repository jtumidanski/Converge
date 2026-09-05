import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import type { ListDocument, PageMeta, Document, Resource } from "@/types/api/jsonapi";

export const server = setupServer();
export { http, HttpResponse };

/** Builds a single-resource JSON:API document for tests. */
export function oneDoc<TType extends string, TAttrs>(
  type: TType,
  id: string,
  attributes: TAttrs,
): Document<Resource<TType, TAttrs>> {
  return { data: { type, id, attributes } };
}

/** Builds a collection JSON:API document for tests. */
export function listDoc<T>(data: T[], page?: PageMeta): ListDocument<T> {
  return page ? { data, meta: { page } } : { data };
}

/** Builds an error document for tests. */
export function errorDoc(status: number, code: string, title: string, detail?: string) {
  return { errors: [{ status: String(status), code, title, detail }] };
}
