import type { Change } from "@/types/models/change";

function mergedAtMs(change: Change): number {
  const raw = change.attributes.mergedAt;
  if (raw === null) return Number.POSITIVE_INFINITY;
  const ms = Date.parse(raw);
  return Number.isNaN(ms) ? Number.POSITIVE_INFINITY : ms;
}

/**
 * applyOrder is the order changes are cherry-picked onto the base: oldest
 * merge first, ties broken by ascending number, unknown merge times last.
 * This is the order the selection bar numbers its chips in and the order sent
 * to POST /api/reviews.
 */
export function applyOrder(changes: Iterable<Change>): Change[] {
  return [...changes].sort((a, b) => {
    const aMs = mergedAtMs(a);
    const bMs = mergedAtMs(b);
    // Compare instants only when they differ; this avoids Infinity - Infinity
    // (NaN, an unspecified comparator result) when both merge times are
    // unknown, falling through to the number tie-break instead.
    if (aMs !== bMs) return aMs - bMs;
    return a.attributes.number - b.attributes.number;
  });
}
