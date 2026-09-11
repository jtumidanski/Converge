import { afterEach, describe, expect, it, vi } from "vitest";
import { createStore, removeKeys } from "@/lib/storage/store";

const isNumbers = (v: unknown): v is number[] =>
  Array.isArray(v) && v.every((n) => typeof n === "number");

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("createStore", () => {
  it("returns the fallback when the key is absent", () => {
    const store = createStore("t.absent", isNumbers, () => [1]);
    expect(store.get()).toEqual([1]);
  });

  it("returns the fallback for malformed JSON", () => {
    localStorage.setItem("t.malformed", "{not json");
    const store = createStore("t.malformed", isNumbers, () => []);
    expect(store.get()).toEqual([]);
  });

  it("returns the fallback for a wrong-shaped value", () => {
    localStorage.setItem("t.shape", JSON.stringify(["a", "b"]));
    const store = createStore("t.shape", isNumbers, () => []);
    expect(store.get()).toEqual([]);
  });

  it("returns the fallback when storage throws on read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("disabled");
    });
    const store = createStore("t.throws", isNumbers, () => [7]);
    expect(store.get()).toEqual([7]);
  });

  it("round-trips a written value", () => {
    const store = createStore("t.write", isNumbers, () => []);
    store.set([1, 2]);
    expect(store.get()).toEqual([1, 2]);
    expect(JSON.parse(localStorage.getItem("t.write") as string)).toEqual([1, 2]);
  });

  it("accepts an updater function", () => {
    const store = createStore("t.update", isNumbers, () => [1]);
    store.set((prev) => [...prev, 2]);
    expect(store.get()).toEqual([1, 2]);
  });

  it("notifies subscribers on write and stops after unsubscribe", () => {
    const store = createStore("t.subscribe", isNumbers, () => []);
    const seen = vi.fn();
    const unsubscribe = store.subscribe(seen);
    store.set([1]);
    expect(seen).toHaveBeenCalledTimes(1);
    unsubscribe();
    store.set([2]);
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it("does not throw when storage rejects a write", () => {
    const store = createStore("t.quota", isNumbers, () => []);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => store.set([1])).not.toThrow();
  });
});

describe("removeKeys", () => {
  it("removes only the keys matching the predicate", () => {
    localStorage.setItem("keep.me", "1");
    localStorage.setItem("drop.a", "1");
    localStorage.setItem("drop.b", "1");
    removeKeys((key) => key.startsWith("drop."));
    expect(localStorage.getItem("keep.me")).toBe("1");
    expect(localStorage.getItem("drop.a")).toBeNull();
    expect(localStorage.getItem("drop.b")).toBeNull();
  });
});
