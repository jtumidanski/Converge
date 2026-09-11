import { ticketKey } from "@/lib/changes/ticketKey";
import { strings } from "@/lib/strings";
import type { Change } from "@/types/models/change";

export const NO_TICKET_LABEL = strings.noTicket;

export interface TicketGroup {
  /** null for the catch-all group. */
  key: string | null;
  label: string;
  changes: Change[];
}

function newestMs(changes: Change[]): number {
  let newest = Number.NEGATIVE_INFINITY;
  for (const change of changes) {
    const raw = change.attributes.mergedAt;
    if (raw === null) continue;
    const ms = Date.parse(raw);
    if (!Number.isNaN(ms) && ms > newest) newest = ms;
  }
  return newest;
}

/**
 * groupByTicket buckets changes by their ticket key, ordering groups by the
 * newest change in each and pinning the catch-all group last (FR-23). Within a
 * group, server order is preserved.
 */
export function groupByTicket(changes: Change[]): TicketGroup[] {
  const buckets = new Map<string, Change[]>();
  const untagged: Change[] = [];
  for (const change of changes) {
    const key = ticketKey(change.attributes.title);
    if (key === null) {
      untagged.push(change);
      continue;
    }
    const bucket = buckets.get(key);
    if (bucket) bucket.push(change);
    else buckets.set(key, [change]);
  }
  const groups: TicketGroup[] = [...buckets.entries()]
    .map(([key, items]) => ({ key, label: key, changes: items }))
    .sort((a, b) => newestMs(b.changes) - newestMs(a.changes));
  if (untagged.length > 0) {
    groups.push({ key: null, label: NO_TICKET_LABEL, changes: untagged });
  }
  return groups;
}
