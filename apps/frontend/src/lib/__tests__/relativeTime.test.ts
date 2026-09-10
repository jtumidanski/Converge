import { describe, expect, it } from "vitest";
import { expiryLabel, relativeTime } from "@/lib/relativeTime";

const now = new Date("2026-09-10T12:00:00Z");

function offset(ms: number): string {
  return new Date(now.getTime() + ms).toISOString();
}

describe("relativeTime", () => {
  it("formats a timestamp seconds in the past", () => {
    expect(relativeTime(offset(-40_000), now)).toMatch(/second/);
  });

  it("formats a timestamp minutes in the past", () => {
    expect(relativeTime(offset(-12 * 60_000), now)).toMatch(/12 minutes ago/);
  });

  it("formats a timestamp hours in the past", () => {
    expect(relativeTime(offset(-5 * 3_600_000), now)).toMatch(/5 hours ago/);
  });

  it("formats a timestamp days in the past", () => {
    expect(relativeTime(offset(-3 * 86_400_000), now)).toMatch(/3 days ago/);
  });

  it("returns an empty string for an unparseable timestamp", () => {
    expect(relativeTime("not-a-date", now)).toBe("");
  });
});

describe("expiryLabel", () => {
  it("flags an expiry 59 minutes away as near", () => {
    const result = expiryLabel(offset(59 * 60_000), now);
    expect(result.nearExpiry).toBe(true);
    expect(result.text).toMatch(/minute/);
  });

  // The boundary is strict: exactly 60 minutes remaining is NOT near-expiry.
  it("does not flag an expiry exactly 60 minutes away as near", () => {
    expect(expiryLabel(offset(60 * 60_000), now).nearExpiry).toBe(false);
  });

  it("does not flag an expiry 61 minutes away as near", () => {
    const result = expiryLabel(offset(61 * 60_000), now);
    expect(result.nearExpiry).toBe(false);
    expect(result.text).toMatch(/hour|minute/);
  });

  it("renders an already-past expiry as expired rather than a negative duration", () => {
    const result = expiryLabel(offset(-5 * 60_000), now);
    expect(result.text).toBe("expired");
    expect(result.nearExpiry).toBe(true);
    expect(result.text).not.toMatch(/-/);
  });

  it("renders an expiry exactly at now as expired", () => {
    expect(expiryLabel(offset(0), now).text).toBe("expired");
  });

  it("returns an empty label for an unparseable timestamp", () => {
    expect(expiryLabel("not-a-date", now)).toEqual({
      text: "",
      nearExpiry: false,
    });
  });
});
