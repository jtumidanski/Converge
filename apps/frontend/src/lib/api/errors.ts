/** ApiError carries the JSON:API error status, code and detail. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail: string;

  constructor(status: number, code: string, title: string, detail: string) {
    super(detail || title);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.detail = detail || title;
  }
}

export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError;
}

/** messageFor returns a user-facing message, falling back for unknown errors. */
export function messageFor(value: unknown, fallback: string): string {
  return isApiError(value) ? value.detail : fallback;
}
