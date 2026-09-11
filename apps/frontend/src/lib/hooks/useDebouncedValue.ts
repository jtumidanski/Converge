import { useEffect, useState } from "react";

/**
 * useDebouncedValue trails `value` by `delayMs`, restarting on every change so
 * only the last value in a burst propagates. Type-ahead search uses this to
 * keep the provider page-walk off the critical path of every keystroke.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}
