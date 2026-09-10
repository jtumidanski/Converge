import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FileTree } from "@/components/features/review/FileTree";
import type { ReviewFile } from "@/types/models/reviewFile";

function file(
  path: string,
  status: ReviewFile["attributes"]["status"],
  additions: number,
  deletions: number,
): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: { path, previousPath: "", status, additions, deletions, binary: false },
  };
}

describe("FileTree", () => {
  const files = [
    file("src/field/FieldService.java", "modified", 40, 12),
    file("src/field/FieldMapper.java", "added", 10, 0),
    file("README.md", "deleted", 0, 5),
  ];

  it("groups files by directory and shows counts", () => {
    render(<FileTree files={files} selectedPath="README.md" onSelect={vi.fn()} />);
    expect(screen.getByText("src/field")).toBeInTheDocument();
    expect(screen.getByText("FieldService.java")).toBeInTheDocument();
    expect(screen.getByText("+40")).toBeInTheDocument();
    expect(screen.getByText("−12")).toBeInTheDocument();
    expect(screen.getAllByText(/added|modified|deleted/i).length).toBeGreaterThanOrEqual(3);
  });

  it("marks the selected file and reports clicks", async () => {
    const onSelect = vi.fn();
    render(<FileTree files={files} selectedPath="README.md" onSelect={onSelect} />);
    const selected = screen.getByRole("button", { name: /README\.md/ });
    expect(selected).toHaveAttribute("aria-current", "true");
    await userEvent.click(screen.getByRole("button", { name: /FieldMapper\.java/ }));
    expect(onSelect).toHaveBeenCalledWith("src/field/FieldMapper.java");
  });
});
