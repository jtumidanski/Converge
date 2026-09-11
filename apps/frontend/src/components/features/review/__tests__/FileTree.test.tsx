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
  // A second file alongside App.java so "src/main/java/com/atlas" is a
  // directory with 2+ children -- exercises sibling rendering, not just
  // buildTree's own (already-tested) sort.
  file("src/main/java/com/atlas/Util.java", "modified"),
  file("src/test/AppTest.java", "added"),
  // A second sibling under src/test for the same reason.
  file("src/test/UtilTest.java", "added"),
  file("README.md", "deleted"),
  // A sibling pair one level shallower than the atlas/test files, so
  // indentation assertions have two distinct depths to compare.
  file("docs/guide/Intro.md", "modified"),
  file("docs/guide/Setup.md", "modified"),
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

  it("renders sibling files in sorted order and indents deeper files further", () => {
    renderTree();
    const rows = screen.getAllByRole("treeitem");
    const paths = rows.map((row) => row.getAttribute("data-path"));
    const atlasAppIndex = paths.indexOf("src/main/java/com/atlas/App.java");
    const atlasUtilIndex = paths.indexOf("src/main/java/com/atlas/Util.java");
    const testAppIndex = paths.indexOf("src/test/AppTest.java");
    const testUtilIndex = paths.indexOf("src/test/UtilTest.java");
    // Sibling order within each directory follows the alphabetical sort
    // buildTree produces: App.java before Util.java, AppTest before UtilTest.
    expect(atlasAppIndex).toBeGreaterThanOrEqual(0);
    expect(atlasAppIndex).toBeLessThan(atlasUtilIndex);
    expect(testAppIndex).toBeLessThan(testUtilIndex);

    const introRow = screen.getByRole("treeitem", { name: "docs/guide/Intro.md" });
    const appRow = screen.getByRole("treeitem", { name: "src/main/java/com/atlas/App.java" });
    const utilRow = screen.getByRole("treeitem", { name: "src/main/java/com/atlas/Util.java" });
    // docs/guide/Intro.md sits one directory level shallower (depth 1) than
    // src/main/java/com/atlas/App.java (depth 2, since "src" and the
    // collapsed "main/java/com/atlas" chain are each their own row). If a
    // child were rendered at its parent's depth instead of depth + 1, these
    // two values would collide.
    expect(introRow.style.paddingLeft).toBe("1.25rem");
    expect(appRow.style.paddingLeft).toBe("2rem");
    // Siblings in the same directory share a depth.
    expect(utilRow.style.paddingLeft).toBe(appRow.style.paddingLeft);
  });

  it("expands multiple levels of collapsed ancestors when the selection moves into them", async () => {
    const nested = [
      file("root/branch-a/leaf-a/FileA.java", "modified"),
      file("root/branch-a/leaf-b/FileB.java", "modified"),
      file("root/other-branch/OtherFile.java", "modified"),
    ];
    const onSelect = vi.fn();
    const onToggleViewed = vi.fn();
    const { rerender } = render(
      <FileTree
        files={nested}
        viewed={new Set<string>()}
        selectedPath="root/other-branch/OtherFile.java"
        onSelect={onSelect}
        onToggleViewed={onToggleViewed}
      />,
    );

    // Collapse the inner directory first (root is still expanded so its row
    // is reachable), then collapse root itself -- two independent levels of
    // ancestor above FileA.java are now both collapsed.
    await userEvent.click(screen.getByRole("button", { name: "Collapse root/branch-a" }));
    await userEvent.click(screen.getByRole("button", { name: "Collapse root" }));
    expect(screen.queryByText("FileA.java")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /branch-a/i })).not.toBeInTheDocument();

    // The selection moves (e.g. via j/k or the footer) into the file
    // buried under both collapsed ancestors.
    rerender(
      <FileTree
        files={nested}
        viewed={new Set<string>()}
        selectedPath="root/branch-a/leaf-a/FileA.java"
        onSelect={onSelect}
        onToggleViewed={onToggleViewed}
      />,
    );

    expect(screen.getByText("FileA.java")).toBeInTheDocument();
    expect(
      screen.getByRole("treeitem", { name: "root/branch-a/leaf-a/FileA.java" }),
    ).toHaveAttribute("aria-selected", "true");
  });
});
