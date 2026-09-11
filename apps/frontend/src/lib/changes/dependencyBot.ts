import type { Change } from "@/types/models/change";

const BOT_PREFIXES = ["renovate/", "dependabot/"];

/**
 * isDependencyBot keys off the source branch rather than the author, because
 * both tools are frequently configured to push under a human's token while
 * always using their own branch prefix (FR-21).
 */
export function isDependencyBot(change: Change): boolean {
  const branch = change.attributes.sourceBranch.toLowerCase();
  return BOT_PREFIXES.some((prefix) => branch.startsWith(prefix));
}
