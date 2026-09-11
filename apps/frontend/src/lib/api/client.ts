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

/**
 * onUnauthorized is invoked when any request answers 401.
 *
 * A module-level callback rather than a thrown-and-caught event keeps this
 * file free of React and router imports, preserving its shape as a thin fetch
 * wrapper. AuthProvider registers the callback that clears the query cache and
 * navigates to /login.
 */
let onUnauthorized: (() => void) | null = null;

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler;
}

async function request(path: string, init: RequestInit): Promise<Response> {
  const response = await fetch(path, {
    ...init,
    // The login session lives in an HttpOnly cookie, so it is invisible to
    // JS and must be sent by the browser. Nothing is read from or written to
    // localStorage or sessionStorage (FR-8.8).
    credentials: "include",
    headers: { Accept: JSON_API, ...(init.headers ?? {}) },
  });
  if (!response.ok) {
    const error = await toApiError(response);
    if (response.status === 401) {
      onUnauthorized?.();
    }
    throw error;
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

export async function apiPatch<T>(path: string, body: unknown): Promise<T> {
  const response = await request(path, {
    method: "PATCH",
    headers: { "Content-Type": JSON_API },
    body: JSON.stringify(body),
  });
  return (await response.json()) as T;
}

/** apiPostNoContent posts and discards a 204 response. */
export async function apiPostNoContent(path: string, body?: unknown): Promise<void> {
  await request(path, {
    method: "POST",
    ...(body === undefined
      ? {}
      : { headers: { "Content-Type": JSON_API }, body: JSON.stringify(body) }),
  });
}

/**
 * apiDeleteWithBody exists for DELETE /api/auth/me, which takes a body
 * because account deletion is irreversible and must be password-confirmed.
 */
export async function apiDeleteWithBody(path: string, body: unknown): Promise<void> {
  await request(path, {
    method: "DELETE",
    headers: { "Content-Type": JSON_API },
    body: JSON.stringify(body),
  });
}
