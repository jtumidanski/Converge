import { useCallback, useSyncExternalStore } from "react";

export interface Store<T> {
  readonly key: string;
  get(): T;
  set(next: T | ((prev: T) => T)): void;
  subscribe(callback: () => void): () => void;
}

/**
 * createStore wraps one localStorage key behind a total reader.
 *
 * Every failure mode -- absent key, malformed JSON, right JSON of the wrong
 * shape, and a storage that throws outright (private browsing, disabled
 * storage, exhausted quota) -- collapses to `fallback()`. This is the same
 * contract lib/theme/storage.ts established; that module keeps its own copy
 * because of its boot-script coupling, which this abstraction must not disturb.
 *
 * The value is cached so `get()` is referentially stable between writes, which
 * useSyncExternalStore requires: re-parsing on every call would hand React a
 * new array identity each render and loop forever.
 */
export function createStore<T>(
  key: string,
  isValid: (value: unknown) => value is T,
  fallback: () => T,
): Store<T> {
  const listeners = new Set<() => void>();
  let cache: T | undefined;
  let loaded = false;

  function read(): T {
    try {
      const raw = localStorage.getItem(key);
      if (raw === null) return fallback();
      const parsed: unknown = JSON.parse(raw);
      return isValid(parsed) ? parsed : fallback();
    } catch {
      return fallback();
    }
  }

  function get(): T {
    if (!loaded) {
      cache = read();
      loaded = true;
    }
    return cache as T;
  }

  function emit(): void {
    for (const listener of listeners) listener();
  }

  function set(next: T | ((prev: T) => T)): void {
    const value = typeof next === "function" ? (next as (prev: T) => T)(get()) : next;
    cache = value;
    loaded = true;
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // Best effort: the session stays correct in memory and a failed write
      // must never break the interaction that triggered it.
    }
    emit();
  }

  function subscribe(callback: () => void): () => void {
    listeners.add(callback);
    if (listeners.size === 1) {
      window.addEventListener("storage", onStorage);
    }
    return () => {
      listeners.delete(callback);
      if (listeners.size === 0) {
        window.removeEventListener("storage", onStorage);
      }
    };
  }

  // Another tab wrote this key: drop the cache so the next get() re-reads.
  function onStorage(event: StorageEvent): void {
    if (event.key !== null && event.key !== key) return;
    loaded = false;
    emit();
  }

  return { key, get, set, subscribe };
}

/** useStore binds a Store to React, re-rendering on every write. */
export function useStore<T>(store: Store<T>): [T, Store<T>["set"]] {
  const subscribe = useCallback((cb: () => void) => store.subscribe(cb), [store]);
  const getSnapshot = useCallback(() => store.get(), [store]);
  const value = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  const set = useCallback<Store<T>["set"]>((next) => store.set(next), [store]);
  return [value, set];
}

/** removeKeys deletes every localStorage key matching predicate. Total. */
export function removeKeys(predicate: (key: string) => boolean): void {
  try {
    const doomed: string[] = [];
    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i);
      if (key !== null && predicate(key)) doomed.push(key);
    }
    for (const key of doomed) localStorage.removeItem(key);
  } catch {
    // Storage unavailable; there is nothing to prune.
  }
}
