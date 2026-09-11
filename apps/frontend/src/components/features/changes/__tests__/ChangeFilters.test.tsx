import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ChangeFilters } from "@/components/features/changes/ChangeFilters";

function renderFilters(overrides: Partial<React.ComponentProps<typeof ChangeFilters>> = {}) {
  const props = {
    search: "",
    onSearchChange: vi.fn(),
    authors: ["adam", "zoe"],
    activeAuthors: new Set<string>(),
    onToggleAuthor: vi.fn(),
    hideBots: true,
    onHideBotsChange: vi.fn(),
    groupByTicket: false,
    onGroupByTicketChange: vi.fn(),
    shown: 3,
    total: 10,
    ...overrides,
  };
  render(<ChangeFilters {...props} />);
  return props;
}

describe("ChangeFilters", () => {
  it("renders one chip per author", () => {
    renderFilters();
    expect(screen.getByRole("button", { name: "adam" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "zoe" })).toBeInTheDocument();
  });

  it("reports an author chip toggle", async () => {
    const props = renderFilters();
    await userEvent.click(screen.getByRole("button", { name: "zoe" }));
    expect(props.onToggleAuthor).toHaveBeenCalledWith("zoe");
  });

  it("marks active author chips as pressed", () => {
    renderFilters({ activeAuthors: new Set(["zoe"]) });
    expect(screen.getByRole("button", { name: "zoe" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "adam" })).toHaveAttribute("aria-pressed", "false");
  });

  it("toggles hide-dependency-bots", async () => {
    const props = renderFilters();
    await userEvent.click(screen.getByRole("switch", { name: /hide dependency bots/i }));
    expect(props.onHideBotsChange).toHaveBeenCalledWith(false);
  });

  it("marks the hide-dependency-bots switch as checked to reflect the prop", () => {
    renderFilters({ hideBots: true });
    expect(screen.getByRole("switch", { name: /hide dependency bots/i })).toBeChecked();
  });

  it("marks the hide-dependency-bots switch as unchecked to reflect the prop", () => {
    renderFilters({ hideBots: false });
    expect(screen.getByRole("switch", { name: /hide dependency bots/i })).not.toBeChecked();
  });

  it("toggles group-by-ticket", async () => {
    const props = renderFilters();
    await userEvent.click(screen.getByRole("button", { name: /group by ticket/i }));
    expect(props.onGroupByTicketChange).toHaveBeenCalledWith(true);
  });

  it("marks the group-by-ticket button as pressed to reflect the prop", () => {
    renderFilters({ groupByTicket: true });
    expect(screen.getByRole("button", { name: /group by ticket/i })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("marks the group-by-ticket button as unpressed to reflect the prop", () => {
    renderFilters({ groupByTicket: false });
    expect(screen.getByRole("button", { name: /group by ticket/i })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("shows the filtered count", () => {
    renderFilters();
    expect(screen.getByText("3 of 10 shown")).toBeInTheDocument();
  });

  it("passes the search value through to ChangeSearch", () => {
    renderFilters({ search: "needle" });
    expect(screen.getByRole("textbox", { name: /search/i })).toHaveValue("needle");
  });
});
