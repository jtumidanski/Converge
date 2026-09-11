import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ChangeTable, buildRows } from "@/components/features/changes/ChangeTable";
import { groupByTicket } from "@/lib/changes/groupByTicket";
import type { Change } from "@/types/models/change";

function change(number: number, title: string, author = "jsmith"): Change {
  return {
    type: "changes",
    id: String(number),
    attributes: {
      number,
      title,
      author,
      sourceBranch: `feat/${number}`,
      targetBranch: "main",
      mergedAt: "2026-01-01T00:00:00Z",
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: `https://example.test/mr/${number}`,
    },
  };
}

const changes = [change(1, "ATLAS-1 first"), change(2, "ATLAS-1 second"), change(3, "untagged")];

function renderTable(props: Partial<React.ComponentProps<typeof ChangeTable>> = {}) {
  const isSelected = props.isSelected ?? (() => false);
  const onToggle = vi.fn();
  const onToggleGroup = vi.fn();
  const onToggleAll = vi.fn();
  const onShowBots = vi.fn();
  render(
    <ChangeTable
      rows={buildRows(changes, null, 0, isSelected)}
      loading={false}
      isSelected={isSelected}
      onToggle={onToggle}
      onToggleGroup={onToggleGroup}
      onToggleAll={onToggleAll}
      allSelected={false}
      someSelected={false}
      onShowBots={onShowBots}
      {...props}
    />,
  );
  return { onToggle, onToggleGroup, onToggleAll, onShowBots };
}

describe("ChangeTable", () => {
  it("renders a flat table with the ticket key as a badge", () => {
    renderTable();
    expect(screen.getByText("#1")).toBeInTheDocument();
    expect(screen.getAllByText("ATLAS-1")).toHaveLength(2);
  });

  it("toggles selection when a row is clicked", async () => {
    const { onToggle } = renderTable();
    await userEvent.click(screen.getByText("ATLAS-1 first"));
    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onToggle.mock.calls[0]?.[0]?.attributes.number).toBe(1);
  });

  it("does not toggle when a link inside the row is clicked", async () => {
    const { onToggle } = renderTable();
    await userEvent.click(screen.getByRole("link", { name: "#1" }));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it("renders group headers with a select-all control when grouping", async () => {
    const { onToggleGroup } = renderTable({
      rows: buildRows(changes, groupByTicket(changes), 0, () => false),
    });
    expect(screen.getByText("ATLAS-1")).toBeInTheDocument();
    expect(screen.getByText("No ticket")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Select all 2" }));
    expect(onToggleGroup).toHaveBeenCalled();
  });

  it("offers Deselect all when the whole group is selected", () => {
    renderTable({
      rows: buildRows(changes, groupByTicket(changes), 0, (n) => n === 1 || n === 2),
      isSelected: (n) => n === 1 || n === 2,
    });
    expect(screen.getByRole("button", { name: "Deselect all" })).toBeInTheDocument();
  });

  it("shows the hidden-bots summary row with a Show button", async () => {
    const { onShowBots } = renderTable({ rows: buildRows(changes, null, 4, () => false) });
    expect(screen.getByText(/4 changes hidden by/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(onShowBots).toHaveBeenCalled();
  });

  it("puts the header checkbox in the indeterminate state when some are selected", () => {
    renderTable({ someSelected: true });
    expect(screen.getByRole("checkbox", { name: /select all visible/i })).toHaveAttribute(
      "data-state",
      "indeterminate",
    );
  });

  it("shows the empty state when nothing matches", () => {
    renderTable({ rows: [] });
    expect(screen.getByText("No merged PRs/MRs")).toBeInTheDocument();
  });

  it("renders the row's author name and source branch text", () => {
    // Distinct from every other fixture string in this file (titles, branch
    // patterns, numbers) so the assertion can only pass if ChangeRow renders
    // the real author/sourceBranch values, not some other field.
    const detailed: Change = {
      type: "changes",
      id: "42",
      attributes: {
        number: 42,
        title: "Row detail regression check",
        author: "morgan-reviewer",
        sourceBranch: "feature/checkout-redesign",
        targetBranch: "main",
        mergedAt: "2026-01-01T00:00:00Z",
        createdAt: "2026-01-01T00:00:00Z",
        landingSha: null,
        webUrl: "https://example.test/mr/42",
      },
    };
    renderTable({ rows: buildRows([detailed], null, 0, () => false) });
    expect(screen.getByText("morgan-reviewer")).toBeInTheDocument();
    expect(screen.getByText("feature/checkout-redesign")).toBeInTheDocument();
  });
});
