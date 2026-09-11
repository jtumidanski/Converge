import { afterEach, describe, expect, it } from "vitest";
import {
  clearViewed,
  pruneViewed,
  toggleViewed,
  viewedKey,
  viewedStore,
} from "@/lib/storage/viewed";

afterEach(() => localStorage.clear());

describe("viewed", () => {
  it("uses the documented key shape", () => {
    expect(viewedKey("abc")).toBe("converge.viewed.abc");
  });

  it("starts empty and toggles a path on and off", () => {
    expect(viewedStore("r1").get()).toEqual([]);
    toggleViewed("r1", "a/b.ts");
    expect(viewedStore("r1").get()).toEqual(["a/b.ts"]);
    toggleViewed("r1", "a/b.ts");
    expect(viewedStore("r1").get()).toEqual([]);
  });

  it("returns the same store instance for the same review id", () => {
    expect(viewedStore("r1")).toBe(viewedStore("r1"));
  });

  it("keeps reviews independent", () => {
    toggleViewed("r1", "a.ts");
    toggleViewed("r2", "b.ts");
    expect(viewedStore("r1").get()).toEqual(["a.ts"]);
    expect(viewedStore("r2").get()).toEqual(["b.ts"]);
  });

  it("clears one review's state", () => {
    toggleViewed("r1", "a.ts");
    clearViewed("r1");
    expect(viewedStore("r1").get()).toEqual([]);
    expect(localStorage.getItem(viewedKey("r1"))).toBeNull();
  });

  it("prunes keys for reviews that are no longer active", () => {
    toggleViewed("live", "a.ts");
    toggleViewed("dead", "b.ts");
    localStorage.setItem("unrelated", "keep");
    pruneViewed(["live"]);
    expect(localStorage.getItem(viewedKey("live"))).not.toBeNull();
    expect(localStorage.getItem(viewedKey("dead"))).toBeNull();
    expect(localStorage.getItem("unrelated")).toBe("keep");
  });

  it("tolerates a malformed stored value", () => {
    localStorage.setItem(viewedKey("bad"), "{oops");
    expect(viewedStore("bad").get()).toEqual([]);
  });

  it("tolerates a stored array whose members are not strings", () => {
    localStorage.setItem(viewedKey("wrong-types"), JSON.stringify([1, 2, 3]));
    expect(viewedStore("wrong-types").get()).toEqual([]);
  });
});
