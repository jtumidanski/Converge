import { createStore, type Store } from "@/lib/storage/store";

export const CHANGE_FILTERS_KEY = "converge.changeFilters";

export interface ChangeFilters {
  /** Hide renovate/ and dependabot/ source branches. On by default (FR-21). */
  hideBots: boolean;
  /** Group the table by ticket key. Off by default (FR-23). */
  groupByTicket: boolean;
}

const DEFAULTS: ChangeFilters = { hideBots: true, groupByTicket: false };

function isChangeFilters(value: unknown): value is ChangeFilters {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return typeof v.hideBots === "boolean" && typeof v.groupByTicket === "boolean";
}

export const changeFiltersStore: Store<ChangeFilters> = createStore(
  CHANGE_FILTERS_KEY,
  isChangeFilters,
  () => ({ ...DEFAULTS }),
);
