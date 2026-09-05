import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useSelection } from "@/lib/hooks/useSelection";
import type { Change } from "@/types/models/change";

function change(n: number): Change {
  return {
    type: "changes",
    id: String(n),
    attributes: {
      number: n,
      title: `change ${n}`,
      author: "dev",
      sourceBranch: "feat",
      targetBranch: "main",
      mergedAt: "2026-08-21T14:02:11Z",
      createdAt: "2026-08-20T09:00:00Z",
      landingSha: null,
      webUrl: "https://example.test",
    },
  };
}

describe("useSelection", () => {
  beforeEach(() => sessionStorage.clear());

  it("toggles, reports membership and produces sorted numbers", () => {
    const { result } = renderHook(() => useSelection("converge:selection:gh/atlas/server"));
    expect(result.current.count).toBe(0);
    act(() => result.current.toggle(change(427)));
    act(() => result.current.toggle(change(421)));
    expect(result.current.numbers).toEqual([421, 427]);
    expect(result.current.isSelected(421)).toBe(true);
    act(() => result.current.toggle(change(421)));
    expect(result.current.isSelected(421)).toBe(false);
    expect(result.current.count).toBe(1);
    act(() => result.current.clear());
    expect(result.current.count).toBe(0);
  });

  it("persists to sessionStorage and restores on remount", () => {
    const key = "converge:selection:gh/atlas/server";
    const first = renderHook(() => useSelection(key));
    act(() => first.result.current.toggle(change(435)));
    first.unmount();
    const second = renderHook(() => useSelection(key));
    expect(second.result.current.numbers).toEqual([435]);
    expect(second.result.current.selected.get(435)?.attributes.title).toBe("change 435");
  });

  it("keeps selections separate per storage key and survives corrupt storage", () => {
    sessionStorage.setItem("converge:selection:gh/other", "not json");
    const { result } = renderHook(() => useSelection("converge:selection:gh/other"));
    expect(result.current.count).toBe(0);
    act(() => result.current.toggle(change(1)));
    const other = renderHook(() => useSelection("converge:selection:gh/atlas/server"));
    expect(other.result.current.count).toBe(0);
  });
});
