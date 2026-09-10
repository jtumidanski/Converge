import { useCallback, useEffect, useMemo, useState } from "react";
import type { Change } from "@/types/models/change";

function readStorage(key: string): Map<number, Change> {
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw) return new Map();
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Map();
    const entries = parsed.filter(
      (item): item is Change =>
        typeof item === "object" && item !== null && typeof (item as Change).id === "string",
    );
    return new Map(entries.map((item) => [item.attributes.number, item]));
  } catch {
    return new Map();
  }
}

export interface SelectionState {
  selected: Map<number, Change>;
  isSelected: (n: number) => boolean;
  toggle: (change: Change) => void;
  clear: () => void;
  numbers: number[];
  count: number;
}

/** useSelection keeps selected PRs/MRs across pagination and search, backed by sessionStorage. */
export function useSelection(storageKey: string): SelectionState {
  const [state, setState] = useState<{ key: string; selected: Map<number, Change> }>(() => ({
    key: storageKey,
    selected: readStorage(storageKey),
  }));

  // Storage key changed (e.g. switching repositories): re-derive state during render
  // rather than in an effect, per https://react.dev/learn/you-might-not-need-an-effect.
  const selected = state.key === storageKey ? state.selected : readStorage(storageKey);
  if (state.key !== storageKey) {
    setState({ key: storageKey, selected });
  }

  const setSelected = useCallback(
    (updater: (current: Map<number, Change>) => Map<number, Change>) => {
      setState((current) => ({ key: current.key, selected: updater(current.selected) }));
    },
    [],
  );

  useEffect(() => {
    try {
      sessionStorage.setItem(storageKey, JSON.stringify([...selected.values()]));
    } catch {
      // Storage is best effort; selection still works in memory.
    }
  }, [storageKey, selected]);

  const toggle = useCallback(
    (change: Change) => {
      setSelected((current) => {
        const next = new Map(current);
        if (next.has(change.attributes.number)) {
          next.delete(change.attributes.number);
        } else {
          next.set(change.attributes.number, change);
        }
        return next;
      });
    },
    [setSelected],
  );

  const clear = useCallback(() => setSelected(() => new Map()), [setSelected]);

  const numbers = useMemo(() => [...selected.keys()].sort((a, b) => a - b), [selected]);

  const isSelected = useCallback((n: number) => selected.has(n), [selected]);

  return {
    selected,
    isSelected,
    toggle,
    clear,
    numbers,
    count: selected.size,
  };
}
