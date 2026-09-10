import { ApiError } from "@/lib/api/errors";
import { isErrorDocument } from "@/types/api/jsonapi";

const JSON_API = "application/vnd.api+json";

async function toApiError(response: Response): Promise<ApiError> {
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    // Not a JSON body (e.g. an HTML error page from a proxy): body stays null.
  }
  if (isErrorDocument(body)) {
    const first = body.errors[0];
    if (first) {
      return new ApiError(response.status, first.code, first.title, first.detail ?? "");
    }
  }
  return new ApiError(response.status, "UNKNOWN", response.statusText || "Request failed", "");
}

async function request(path: string, init: RequestInit): Promise<Response> {
  const response = await fetch(path, {
    ...init,
    headers: { Accept: JSON_API, ...(init.headers ?? {}) },
  });
  if (!response.ok) {
    throw await toApiError(response);
  }
  return response;
}

export async function apiGet<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await request(path, { ...init, method: "GET" });
  return (await response.json()) as T;
}

export async function apiGetText(path: string): Promise<string> {
  const response = await request(path, { method: "GET", headers: { Accept: "text/plain" } });
  return await response.text();
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const response = await request(path, {
    method: "POST",
    headers: { "Content-Type": JSON_API },
    body: JSON.stringify(body),
  });
  return (await response.json()) as T;
}

export async function apiDelete(path: string): Promise<void> {
  await request(path, { method: "DELETE" });
}
