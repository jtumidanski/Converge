import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FileTree } from "@/components/features/review/FileTree";
import type { FileStatus, ReviewFile } from "@/types/models/reviewFile";

function file(
  path: string,
  status: FileStatus = "modified",
  additions = 3,
  deletions = 1,
): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: { path, previousPath: "", status, additions, deletions, binary: false },
  };
}

const files = [
  file("src/main/java/com/atlas/App.java", "modified"),
  file("src/test/AppTest.java", "added"),
  file("README.md", "deleted"),
];

function renderTree(props: Partial<React.ComponentProps<typeof FileTree>> = {}) {
  const onSelect = vi.fn();
  const onToggleViewed = vi.fn();
  render(
    <FileTree
      files={files}
      viewed={new Set<string>()}
      selectedPath="README.md"
      onSelect={onSelect}
      onToggleViewed={onToggleViewed}
      {...props}
    />,
  );
  return { onSelect, onToggleViewed };
}

describe("FileTree", () => {
  it("collapses single-child directory chains into one row", () => {
    renderTree();
    expect(screen.getByText("src/main/java/com/atlas")).toBeInTheDocument();
    expect(screen.getByText("App.java")).toBeInTheDocument();
  });

  it("shows the status letter and line counts per file", () => {
    renderTree();
    const row = screen.getByRole("treeitem", { name: /README\.md/ });
    expect(within(row).getByText("D")).toBeInTheDocument();
    expect(within(row).getByText("+3")).toBeInTheDocument();
    expect(within(row).getByText("−1")).toBeInTheDocument();
  });

  it("marks the selected file", () => {
    renderTree();
    expect(screen.getByRole("treeitem", { name: /README\.md/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("reports a pointer selection", async () => {
    const { onSelect } = renderTree();
    await userEvent.click(screen.getByText("App.java"));
    expect(onSelect).toHaveBeenCalledWith("src/main/java/com/atlas/App.java", "pointer");
  });

  it("toggles viewed from the row checkbox without selecting the file", async () => {
    const { onSelect, onToggleViewed } = renderTree();
    await userEvent.click(screen.getByRole("checkbox", { name: /mark App\.java viewed/i }));
    expect(onToggleViewed).toHaveBeenCalledWith("src/main/java/com/atlas/App.java");
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("renders viewed files muted and checked", () => {
    renderTree({ viewed: new Set(["README.md"]) });
    expect(screen.getByRole("checkbox", { name: /mark README\.md viewed/i })).toBeChecked();
  });

  it("collapses and expands a directory", async () => {
    renderTree();
    await userEvent.click(screen.getByRole("button", { name: /collapse src\/test/i }));
    expect(screen.queryByText("AppTest.java")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /expand src\/test/i }));
    expect(screen.getByText("AppTest.java")).toBeInTheDocument();
  });

  it("filters rows by a substring of the full path", async () => {
    renderTree();
    await userEvent.type(screen.getByPlaceholderText(/filter files/i), "test");
    expect(screen.getByText("AppTest.java")).toBeInTheDocument();
    expect(screen.queryByText("App.java")).not.toBeInTheDocument();
    expect(screen.queryByText("README.md")).not.toBeInTheDocument();
  });
});
