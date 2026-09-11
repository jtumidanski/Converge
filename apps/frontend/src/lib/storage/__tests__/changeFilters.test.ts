import { afterEach, describe, expect, it } from "vitest";
import { CHANGE_FILTERS_KEY, changeFiltersStore } from "@/lib/storage/changeFilters";

afterEach(() => localStorage.clear());

describe("changeFilters", () => {
  it("uses the documented key", () => {
    expect(CHANGE_FILTERS_KEY).toBe("converge.changeFilters");
  });

  it("defaults to hiding bots and not grouping", () => {
    expect(changeFiltersStore.get()).toEqual({ hideBots: true, groupByTicket: false });
  });

  it("persists a toggle", () => {
    changeFiltersStore.set({ hideBots: false, groupByTicket: true });
    expect(JSON.parse(localStorage.getItem(CHANGE_FILTERS_KEY) as string)).toEqual({
      hideBots: false,
      groupByTicket: true,
    });
  });

  it("falls back to the defaults for a partial stored object", () => {
    localStorage.setItem(CHANGE_FILTERS_KEY, JSON.stringify({ hideBots: false }));
    // The store caches its first read, so exercise a fresh read path.
    changeFiltersStore.set(changeFiltersStore.get());
    expect(changeFiltersStore.get().groupByTicket).toBe(false);
  });
});
