/**
 * Intentionally stricter than gitx.ValidateRepoFullName: owner/name, no
 * traversal, no leading -, . or /. The backend only rejects a leading `/`,
 * `-`, or `.` on the whole string plus `.`/`..`/`..`-containing segments; this
 * client rule additionally rejects any segment that merely starts with `.`
 * or `-` (e.g. it blocks `org/.github`, which the backend would accept).
 */
const REPO_NAME = /^[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)+$/;

export function isValidRepositoryName(text: string): boolean {
  if (!REPO_NAME.test(text)) return false;
  if (/^[-./]/.test(text)) return false;
  return text
    .split("/")
    .every((segment) => segment !== "" && !segment.startsWith(".") && !segment.startsWith("-"));
}

/**
 * parseRepositoryInput turns what the reviewer typed or pasted into a
 * repository full name, or null when it is neither.
 *
 * Accepted: a bare `owner/name` (any depth, as GitLab subgroups need), and a
 * URL under the selected provider's base URL. A URL on any other origin is
 * rejected rather than guessed at -- resolving it against the wrong provider
 * would produce a confusing 404.
 */
export function parseRepositoryInput(text: string, baseUrl: string | undefined): string | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  if (!trimmed.includes("://")) {
    return isValidRepositoryName(trimmed) ? trimmed : null;
  }
  if (baseUrl === undefined || baseUrl === "") return null;
  let url: URL;
  let base: URL;
  try {
    url = new URL(trimmed);
    base = new URL(baseUrl);
  } catch {
    return null;
  }
  if (url.origin !== base.origin) return null;
  const path = url.pathname
    .replace(/^\/+/, "")
    .replace(/\/+$/, "")
    .replace(/\.git$/, "");
  return path !== "" && isValidRepositoryName(path) ? path : null;
}
