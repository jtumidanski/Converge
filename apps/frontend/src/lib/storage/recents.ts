import { createStore, type Store } from "@/lib/storage/store";

export const RECENTS_KEY = "converge.recentRepositories";

/** MAX_RECENTS caps the stored list; recentsFor caps what the drawer shows. */
const MAX_RECENTS = 20;
const DEFAULT_VISIBLE = 10;

export interface RecentRepository {
  provider: string;
  repository: string;
  defaultBranch: string;
  /** ISO timestamp of the most recent open. */
  openedAt: string;
}

function isRecent(value: unknown): value is RecentRepository {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.provider === "string" &&
    typeof v.repository === "string" &&
    typeof v.defaultBranch === "string" &&
    typeof v.openedAt === "string"
  );
}

function isRecents(value: unknown): value is RecentRepository[] {
  return Array.isArray(value) && value.every(isRecent);
}

export const recentsStore: Store<RecentRepository[]> = createStore(
  RECENTS_KEY,
  isRecents,
  () => [],
);

/**
 * recordRecent moves an entry to the front of the list, replacing any earlier
 * visit to the same (provider, repository) pair. The same repository under two
 * providers is two entries -- the pair, not the name, is the identity.
 */
export function recordRecent(entry: RecentRepository): void {
  recentsStore.set((previous) => {
    const rest = previous.filter(
      (r) => !(r.provider === entry.provider && r.repository === entry.repository),
    );
    return [entry, ...rest].slice(0, MAX_RECENTS);
  });
}

/** recentsFor returns this provider's recents, most recent first. */
export function recentsFor(provider: string, limit: number = DEFAULT_VISIBLE): RecentRepository[] {
  return recentsStore
    .get()
    .filter((r) => r.provider === provider)
    .slice(0, limit);
}

/** mostRecentProvider is the provider of the most recently opened repository. */
export function mostRecentProvider(): string | undefined {
  return recentsStore.get()[0]?.provider;
}
