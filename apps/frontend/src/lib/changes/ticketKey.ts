/**
 * TICKET_KEY is the fixed pattern from FR-22: two or more leading uppercase
 * alphanumerics, a hyphen, then digits. It is deliberately not configurable.
 */
const TICKET_KEY = /\b[A-Z][A-Z0-9]+-\d+\b/;

/** ticketKey returns the first ticket key in a title, or null. */
export function ticketKey(title: string): string | null {
  return TICKET_KEY.exec(title)?.[0] ?? null;
}
