import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SelectionBar } from "@/components/features/changes/SelectionBar";
import type { Change } from "@/types/models/change";

function change(number: number, mergedAt: string | null): Change {
  return {
    type: "changes",
    id: String(number),
    attributes: {
      number,
      title: `Change ${number}`,
      author: "jsmith",
      sourceBranch: "feat/x",
      targetBranch: "main",
      mergedAt,
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: "https://example.test/1",
    },
  };
}

describe("SelectionBar", () => {
  it("collapses to a prompt with nothing selected", () => {
    render(
      <SelectionBar
        selected={[]}
        building={false}
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={vi.fn()}
      />,
    );
    expect(screen.getByText("Select one or more changes")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /build review/i })).not.toBeInTheDocument();
  });

  it("numbers chips in apply order", () => {
    render(
      <SelectionBar
        selected={[change(9, "2026-01-01T00:00:00Z"), change(4, "2026-02-01T00:00:00Z")]}
        building={false}
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={vi.fn()}
      />,
    );
    expect(screen.getByText("2 selected · applied oldest → newest")).toBeInTheDocument();
    const chips = screen.getAllByTestId("selection-chip");
    expect(chips[0]).toHaveTextContent("1");
    expect(chips[0]).toHaveTextContent("#9");
    expect(chips[1]).toHaveTextContent("#4");
  });

  it("removes one change from the selection", async () => {
    // Two distinct changes, and the remove clicked is not the first in apply
    // order: a handler that always removes ordered[0] would still pass with
    // only one selected change (Contract 6's fixture-sharing defect shape).
    const onRemove = vi.fn();
    render(
      <SelectionBar
        selected={[change(9, "2026-01-01T00:00:00Z"), change(4, "2026-02-01T00:00:00Z")]}
        building={false}
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={onRemove}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /remove #4/i }));
    expect(onRemove.mock.calls[0]?.[0]?.attributes.number).toBe(4);
  });

  it("builds and clears", async () => {
    const onBuild = vi.fn();
    const onClear = vi.fn();
    render(
      <SelectionBar
        selected={[change(1, null)]}
        building={false}
        onBuild={onBuild}
        onClear={onClear}
        onRemove={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    expect(onBuild).toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Clear" }));
    expect(onClear).toHaveBeenCalled();
  });

  it("disables both actions while building", () => {
    render(
      <SelectionBar
        selected={[change(1, null)]}
        building
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: /build review/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Clear" })).toBeDisabled();
  });
});
