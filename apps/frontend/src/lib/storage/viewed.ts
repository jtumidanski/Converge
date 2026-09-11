import { createStore, removeKeys, type Store } from "@/lib/storage/store";

const PREFIX = "converge.viewed.";

export function viewedKey(reviewId: string): string {
  return PREFIX + reviewId;
}

function isPaths(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((v) => typeof v === "string");
}

// One store per review id, cached so useStore always sees a stable identity
// for the same review -- a fresh store per render would resubscribe forever.
const stores = new Map<string, Store<string[]>>();

export function viewedStore(reviewId: string): Store<string[]> {
  const existing = stores.get(reviewId);
  if (existing) return existing;
  const store = createStore(viewedKey(reviewId), isPaths, () => []);
  stores.set(reviewId, store);
  return store;
}

/** toggleViewed adds or removes one file path from a review's viewed set. */
export function toggleViewed(reviewId: string, path: string): void {
  viewedStore(reviewId).set((previous) =>
    previous.includes(path) ? previous.filter((p) => p !== path) : [...previous, path],
  );
}

/** clearViewed drops a finished or discarded review's state entirely (FR-38). */
export function clearViewed(reviewId: string): void {
  const store = viewedStore(reviewId);
  store.set([]);
  try {
    localStorage.removeItem(viewedKey(reviewId));
  } catch {
    // Storage unavailable; the in-memory value is already empty.
  }
}

/**
 * pruneViewed removes viewed state for reviews the server no longer lists.
 * Called once per successful GET /api/reviews so an expired or swept review
 * cannot leave its file set behind forever.
 */
export function pruneViewed(activeIds: string[]): void {
  const active = new Set(activeIds.map(viewedKey));
  removeKeys((key) => key.startsWith(PREFIX) && !active.has(key));
  for (const [id] of stores) {
    if (!activeIds.includes(id)) stores.delete(id);
  }
}
