import type { Review } from "@/types/models/review";

/**
 * openInProviderHref resolves the "Open in <provider>" target for the file
 * header. The files endpoint does not attribute a file to the change that
 * introduced it, and per-line attribution is a PRD non-goal, so a single
 * included change gets its own web URL and anything else falls back to the
 * repository (design 2, second deviation).
 */
export function openInProviderHref(
  review: Review,
  repositoryWebUrl: string | undefined,
): string | undefined {
  const included = review.attributes.included;
  if (included.length === 1) {
    const only = included[0];
    if (only && only.webUrl !== "") return only.webUrl;
  }
  return repositoryWebUrl !== undefined && repositoryWebUrl !== "" ? repositoryWebUrl : undefined;
}
