import { describe, expect, it, vi } from "vitest";
import { darkQueryListeners, removeMatchMedia, setSystemDark } from "@/test/matchMedia";

const DARK_QUERY = "(prefers-color-scheme: dark)";

describe("matchMedia stub", () => {
  it("reports light by default", () => {
    expect(window.matchMedia(DARK_QUERY).matches).toBe(false);
  });

  it("reports dark after setSystemDark(true)", () => {
    setSystemDark(true);
    expect(window.matchMedia(DARK_QUERY).matches).toBe(true);
  });

  it("reports false for an unrelated query", () => {
    setSystemDark(true);
    expect(window.matchMedia("(min-width: 600px)").matches).toBe(false);
  });

  it("notifies change listeners with the new value", () => {
    const listener = vi.fn();
    window.matchMedia(DARK_QUERY).addEventListener("change", listener);

    setSystemDark(true);

    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].matches).toBe(true);
    expect(darkQueryListeners.count()).toBe(1);
  });

  it("stops notifying a removed listener", () => {
    const listener = vi.fn();
    const query = window.matchMedia(DARK_QUERY);
    query.addEventListener("change", listener);
    query.removeEventListener("change", listener);

    setSystemDark(true);

    expect(listener).not.toHaveBeenCalled();
    expect(darkQueryListeners.count()).toBe(0);
  });

  it("can be removed entirely for the no-matchMedia fallback", () => {
    removeMatchMedia();
    expect(window.matchMedia).toBeUndefined();
  });
});
