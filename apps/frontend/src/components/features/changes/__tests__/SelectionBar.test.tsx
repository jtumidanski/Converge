import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SelectionBar } from "@/components/features/changes/SelectionBar";

describe("SelectionBar", () => {
  it("disables Clear and Build Review at zero selections, and enables Clear once something is selected", () => {
    const onBuild = vi.fn();
    const onClear = vi.fn();
    render(<SelectionBar count={0} building={false} onBuild={onBuild} onClear={onClear} />);
    expect(screen.getByRole("button", { name: /clear/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /build review/i })).toBeDisabled();
  });

  it("calls onClear, not onBuild, when Clear is clicked with a non-zero selection", async () => {
    const onBuild = vi.fn();
    const onClear = vi.fn();
    render(<SelectionBar count={2} building={false} onBuild={onBuild} onClear={onClear} />);
    const clearButton = screen.getByRole("button", { name: /clear/i });
    expect(clearButton).toBeEnabled();
    await userEvent.click(clearButton);
    expect(onClear).toHaveBeenCalledTimes(1);
    expect(onBuild).not.toHaveBeenCalled();
  });

  it("disables Clear and Build Review while building, even with a non-zero selection", () => {
    render(<SelectionBar count={3} building={true} onBuild={vi.fn()} onClear={vi.fn()} />);
    expect(screen.getByRole("button", { name: /clear/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /build review/i })).toBeDisabled();
  });
});
