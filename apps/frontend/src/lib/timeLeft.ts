import { strings } from "@/lib/strings";

const MINUTE_MS = 60_000;
const HOUR_MS = 3_600_000;

/**
 * timeLeft renders an expiry as a compact "22h left" / "40m left" for the
 * reviews table. It never produces a negative duration: an elapsed expiry
 * reads as "expired", and an unparseable timestamp as an em dash, so a bad
 * value can never render "Invalid Date" in a table cell.
 */
export function timeLeft(expiresAt: string, now: Date = new Date()): string {
  const ms = Date.parse(expiresAt);
  if (Number.isNaN(ms)) return "—";
  const remaining = ms - now.getTime();
  if (remaining <= 0) return strings.expired;
  if (remaining >= HOUR_MS) return strings.hoursLeft(Math.floor(remaining / HOUR_MS));
  if (remaining >= MINUTE_MS) return strings.minutesLeft(Math.floor(remaining / MINUTE_MS));
  return strings.underMinuteLeft;
}
