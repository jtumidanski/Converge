export interface Resource<TType extends string, TAttributes> {
  type: TType;
  id: string;
  attributes: TAttributes;
  relationships?: Record<string, { links: { related: string } }>;
}

export interface Document<T> {
  data: T;
}

export interface PageMeta {
  number: number;
  size: number;
  hasNext: boolean;
}

export interface ListDocument<T> {
  data: T[];
  meta?: { page?: PageMeta };
}

export interface ApiErrorObject {
  status: string;
  code: string;
  title: string;
  detail?: string;
}

export interface ErrorDocument {
  errors: ApiErrorObject[];
}

/** isErrorDocument reports whether value looks like a JSON:API error document. */
export function isErrorDocument(value: unknown): value is ErrorDocument {
  return (
    typeof value === "object" &&
    value !== null &&
    Array.isArray((value as ErrorDocument).errors) &&
    (value as ErrorDocument).errors.length > 0
  );
}

/**
 * unwrapList returns doc.data, throwing rather than silently coercing to []
 * when the server sent a 2xx body that is not actually a JSON:API list
 * document (missing or non-array "data"). A malformed success response must
 * not be presented to a caller as "zero results".
 */
export function unwrapList<T>(doc: ListDocument<T>): T[] {
  if (!Array.isArray(doc.data)) {
    throw new Error("Malformed JSON:API response: expected a list document with a data array.");
  }
  return doc.data;
}

/**
 * unwrapOne returns doc.data, throwing rather than silently coercing to
 * undefined when the server sent a 2xx body missing "data".
 */
export function unwrapOne<T>(doc: Document<T>): T {
  if (doc.data === undefined || doc.data === null) {
    throw new Error("Malformed JSON:API response: expected a single-resource document with data.");
  }
  return doc.data;
}
