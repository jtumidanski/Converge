import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DiffPane } from "@/components/features/review/DiffPane";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

vi.mock("@/components/features/review/FileDiff", () => ({
  FileDiff: () => <div data-testid="file-diff" />,
}));

function diff(path: string): ReviewFileDiff {
  return {
    type: "review-file-diffs",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 12,
      deletions: 3,
      binary: false,
      truncated: false,
      diff: "diff --git a/x b/x\n",
    },
  };
}

function renderPane(props: Partial<React.ComponentProps<typeof DiffPane>> = {}) {
  const onToggleViewed = vi.fn();
  const onNext = vi.fn();
  render(
    <DiffPane
      fileDiff={diff("src/app/main.ts")}
      loading={false}
      href="https://example.test/mr/421"
      providerName="GitLab"
      viewed={false}
      onToggleViewed={onToggleViewed}
      index={0}
      total={3}
      nextName="other.ts"
      onNext={onNext}
      {...props}
    />,
  );
  return { onToggleViewed, onNext };
}

describe("DiffPane", () => {
  it("shows the full path with the file name emphasised and the line counts", () => {
    renderPane();
    expect(screen.getByText("src/app/")).toBeInTheDocument();
    expect(screen.getByText("main.ts")).toBeInTheDocument();
    expect(screen.getByText("+12")).toBeInTheDocument();
    expect(screen.getByText("−3")).toBeInTheDocument();
  });

  it("links out to the provider", () => {
    renderPane();
    expect(screen.getByRole("link", { name: /open in gitlab/i })).toHaveAttribute(
      "href",
      "https://example.test/mr/421",
    );
  });

  it("omits the provider link when there is no target", () => {
    renderPane({ href: undefined });
    expect(screen.queryByRole("link", { name: /open in/i })).not.toBeInTheDocument();
  });

  it("copies the path", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderPane();
    await userEvent.click(screen.getByRole("button", { name: /copy path/i }));
    expect(writeText).toHaveBeenCalledWith("src/app/main.ts");
  });

  it("toggles viewed from the header", async () => {
    const { onToggleViewed } = renderPane();
    await userEvent.click(screen.getByRole("button", { name: /viewed/i }));
    expect(onToggleViewed).toHaveBeenCalled();
  });

  it("shows File i of n and the next file button", async () => {
    const { onNext } = renderPane();
    expect(screen.getByText("File 1 of 3")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /next file: other\.ts/i }));
    expect(onNext).toHaveBeenCalled();
  });

  it("reads Back to first file on the last file", () => {
    renderPane({ index: 2, nextName: "first.ts", isLast: true });
    expect(screen.getByRole("button", { name: /back to first file/i })).toBeInTheDocument();
  });

  it("renders a skeleton while the diff loads", () => {
    renderPane({ loading: true, fileDiff: undefined });
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument();
  });

  it("shows an error banner instead of the diff when loading the file failed", async () => {
    const onRetry = vi.fn();
    renderPane({ error: new Error("boom"), onRetry });
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(onRetry).toHaveBeenCalled();
  });
});
