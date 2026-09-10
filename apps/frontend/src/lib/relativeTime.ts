import { strings } from "@/lib/strings";

/**
 * A single module-scope formatter. Intl constructors are expensive and the
 * resume list re-renders every 2 s while a review is building.
 */
const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

const MINUTE_MS = 60_000;
const HOUR_MS = 3_600_000;
const DAY_MS = 86_400_000;

/** NEAR_EXPIRY_MS is strict: exactly one hour remaining is not near-expiry. */
const NEAR_EXPIRY_MS = HOUR_MS;

const UNITS: ReadonlyArray<[Intl.RelativeTimeFormatUnit, number]> = [
  ["day", DAY_MS],
  ["hour", HOUR_MS],
  ["minute", MINUTE_MS],
  ["second", 1000],
];

/** format renders a signed millisecond delta in the largest unit of magnitude >= 1. */
function format(deltaMs: number): string {
  for (const [unit, size] of UNITS) {
    if (Math.abs(deltaMs) >= size) {
      return formatter.format(Math.round(deltaMs / size), unit);
    }
  }
  return formatter.format(0, "second");
}

/** parse returns the epoch milliseconds of an ISO timestamp, or undefined if unparseable. */
function parse(iso: string): number | undefined {
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? undefined : ms;
}

/**
 * relativeTime formats a past timestamp, e.g. "12 minutes ago". An unparseable
 * timestamp yields an empty string rather than "Invalid Date" (NFR-5).
 */
export function relativeTime(iso: string, now: Date = new Date()): string {
  const ms = parse(iso);
  if (ms === undefined) return "";
  return format(ms - now.getTime());
}

/**
 * expiryLabel formats a future timestamp as a relative fragment ("in 5 hours")
 * and reports whether it is within the near-expiry window. A timestamp at or
 * before `now` yields the word "expired" -- never a negative duration (FR-3.10).
 */
export function expiryLabel(
  iso: string,
  now: Date = new Date(),
): { text: string; nearExpiry: boolean } {
  const ms = parse(iso);
  if (ms === undefined) return { text: "", nearExpiry: false };
  const remaining = ms - now.getTime();
  if (remaining <= 0) return { text: strings.expired, nearExpiry: true };
  return { text: format(remaining), nearExpiry: remaining < NEAR_EXPIRY_MS };
}
