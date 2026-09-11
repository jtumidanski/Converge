import { afterEach, describe, expect, it } from "vitest";
import {
  mostRecentProvider,
  recentsFor,
  recordRecent,
  RECENTS_KEY,
  type RecentRepository,
} from "@/lib/storage/recents";

function entry(
  repository: string,
  provider = "gl",
  openedAt = "2026-01-01T00:00:00Z",
): RecentRepository {
  return { provider, repository, defaultBranch: "main", openedAt };
}

afterEach(() => localStorage.clear());

describe("recents", () => {
  it("uses the documented key", () => {
    expect(RECENTS_KEY).toBe("converge.recentRepositories");
  });

  it("stores the most recent entry first", () => {
    recordRecent(entry("atlas/a"));
    recordRecent(entry("atlas/b"));
    expect(recentsFor("gl").map((r) => r.repository)).toEqual(["atlas/b", "atlas/a"]);
  });

  it("moves an existing entry to the front instead of duplicating it", () => {
    recordRecent(entry("atlas/a"));
    recordRecent(entry("atlas/b"));
    recordRecent(entry("atlas/a", "gl", "2026-02-02T00:00:00Z"));
    const all = recentsFor("gl");
    expect(all.map((r) => r.repository)).toEqual(["atlas/a", "atlas/b"]);
    expect(all[0]?.openedAt).toBe("2026-02-02T00:00:00Z");
  });

  it("treats the same repository under a different provider as a distinct entry", () => {
    recordRecent(entry("atlas/a", "gl"));
    recordRecent(entry("atlas/a", "gh"));
    expect(recentsFor("gl")).toHaveLength(1);
    expect(recentsFor("gh")).toHaveLength(1);
  });

  it("caps the list at twenty entries", () => {
    for (let i = 0; i < 25; i += 1) recordRecent(entry(`atlas/r${i}`));
    expect(recentsFor("gl", 100)).toHaveLength(20);
    expect(recentsFor("gl", 100).at(-1)?.repository).toBe("atlas/r5");
  });

  it("limits the per-provider view to ten by default", () => {
    for (let i = 0; i < 15; i += 1) recordRecent(entry(`atlas/r${i}`));
    expect(recentsFor("gl")).toHaveLength(10);
  });

  it("derives the most recent provider from the first entry", () => {
    recordRecent(entry("atlas/a", "gl"));
    recordRecent(entry("atlas/b", "gh"));
    expect(mostRecentProvider()).toBe("gh");
  });

  it("reports no provider when there are no recents", () => {
    expect(mostRecentProvider()).toBeUndefined();
  });

  it("tolerates a malformed stored value", () => {
    localStorage.setItem(RECENTS_KEY, JSON.stringify([{ nope: true }]));
    expect(recentsFor("gl")).toEqual([]);
  });
});
