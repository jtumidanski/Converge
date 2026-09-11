import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDebouncedValue } from "@/lib/hooks/useDebouncedValue";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useDebouncedValue", () => {
  it("returns the initial value immediately", () => {
    const { result } = renderHook(() => useDebouncedValue("a", 250));
    expect(result.current).toBe("a");
  });

  it("holds the old value until the delay elapses", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "a" },
    });
    rerender({ v: "b" });
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(249));
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(1));
    expect(result.current).toBe("b");
  });

  it("restarts the timer on every change, so only the last value lands", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "a" },
    });
    rerender({ v: "b" });
    act(() => void vi.advanceTimersByTime(200));
    rerender({ v: "c" });
    act(() => void vi.advanceTimersByTime(200));
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(50));
    expect(result.current).toBe("c");
  });
});
