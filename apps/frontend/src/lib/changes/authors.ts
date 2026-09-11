import type { Change } from "@/types/models/change";

/**
 * distinctAuthors is derived from the currently loaded pages only, so loading
 * another page can add chips (FR-20).
 */
export function distinctAuthors(changes: Change[]): string[] {
  const seen = new Set<string>();
  for (const change of changes) {
    const author = change.attributes.author;
    if (author !== "") seen.add(author);
  }
  return [...seen].sort((a, b) => a.localeCompare(b));
}
