import { describe, expect, it } from "vitest";
import { timeLeft } from "@/lib/timeLeft";

const now = new Date("2026-01-01T00:00:00Z");

describe("timeLeft", () => {
  it.each([
    ["2026-01-01T22:00:00Z", "22h left"],
    ["2026-01-01T01:00:00Z", "1h left"],
    ["2026-01-01T00:40:00Z", "40m left"],
    ["2026-01-01T00:00:30Z", "<1m left"],
  ])("formats %s as %s", (expiresAt, expected) => {
    expect(timeLeft(expiresAt, now)).toBe(expected);
  });

  it("rounds hours down so 90 minutes reads as 1h", () => {
    expect(timeLeft("2026-01-01T01:30:00Z", now)).toBe("1h left");
  });

  it("reports an elapsed expiry as expired, never a negative duration", () => {
    expect(timeLeft("2025-12-31T23:00:00Z", now)).toBe("expired");
  });

  it("returns an em dash for an unparseable timestamp", () => {
    expect(timeLeft("not a date", now)).toBe("—");
  });
});
