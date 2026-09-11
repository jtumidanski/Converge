import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { isApiError, messageFor } from "@/lib/api/errors";

/**
 * safeNext returns a path to return to after a successful login, or "/".
 *
 * Only a same-origin absolute path is accepted. An unvalidated ?next is an
 * open redirect: a link to /login?next=https://evil.test would bounce a user
 * off the instance immediately after they authenticate.
 */
export function safeNext(search: string): string {
  const raw = new URLSearchParams(search).get("next");
  if (!raw || !raw.startsWith("/") || raw.startsWith("//")) {
    return "/";
  }
  return raw;
}

/**
 * applyServerError attaches a server error to the field its code identifies,
 * falling back to a form-level error (FR-8.7). INVALID_CREDENTIALS
 * deliberately has no field: the server does not say which of the two is
 * wrong, and guessing here would undo that.
 */
export function applyServerError<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fieldForCode: Partial<Record<string, Path<T>>>,
): void {
  const code = isApiError(error) ? error.code : "";
  const field = fieldForCode[code];
  const message = messageFor(error, "Something went wrong. Try again.");
  if (field) {
    setError(field, { type: "server", message });
    return;
  }
  setError("root" as Path<T>, { type: "server", message });
}
