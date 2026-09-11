import { describe, expect, it } from "vitest";
import { ancestorDirs, buildTree, filterTree, flattenVisible } from "@/lib/review/fileTree";
import type { ReviewFile } from "@/types/models/reviewFile";

function file(path: string): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 1,
      deletions: 1,
      binary: false,
    },
  };
}

describe("buildTree", () => {
  it("collapses a chain of single-child directories into one segment", () => {
    const tree = buildTree([file("src/main/java/com/atlas/App.java")]);
    expect(tree).toHaveLength(1);
    const root = tree[0];
    expect(root?.kind).toBe("dir");
    if (root?.kind !== "dir") throw new Error("expected a directory");
    expect(root.name).toBe("src/main/java/com/atlas");
    expect(root.path).toBe("src/main/java/com/atlas");
    expect(root.children).toHaveLength(1);
    expect(root.children[0]?.kind).toBe("file");
  });

  it("stops collapsing where a directory branches", () => {
    const tree = buildTree([file("src/a/one.ts"), file("src/b/two.ts")]);
    expect(tree).toHaveLength(1);
    const root = tree[0];
    if (root?.kind !== "dir") throw new Error("expected a directory");
    expect(root.name).toBe("src");
    expect(root.children.map((c) => (c.kind === "dir" ? c.name : ""))).toEqual(["a", "b"]);
  });

  it("does not collapse a directory that also holds a file", () => {
    const tree = buildTree([file("src/index.ts"), file("src/lib/util.ts")]);
    const root = tree[0];
    if (root?.kind !== "dir") throw new Error("expected a directory");
    expect(root.name).toBe("src");
    expect(root.children).toHaveLength(2);
  });

  it("sorts directories before files, each by name", () => {
    const tree = buildTree([file("z.ts"), file("a.ts"), file("dir/b.ts")]);
    expect(
      tree.map((n) => (n.kind === "dir" ? `dir:${n.name}` : `file:${n.file.attributes.path}`)),
    ).toEqual(["dir:dir", "file:a.ts", "file:z.ts"]);
  });

  it("puts root-level files at the top level", () => {
    const tree = buildTree([file("README.md")]);
    expect(tree).toHaveLength(1);
    expect(tree[0]?.kind).toBe("file");
  });

  it("returns nothing for no files", () => {
    expect(buildTree([])).toEqual([]);
  });
});

describe("flattenVisible", () => {
  const tree = buildTree([file("src/a/one.ts"), file("src/b/two.ts"), file("root.ts")]);

  it("lists every file path in tree order when nothing is collapsed", () => {
    expect(flattenVisible(tree, new Set())).toEqual(["src/a/one.ts", "src/b/two.ts", "root.ts"]);
  });

  it("drops files under a collapsed directory", () => {
    expect(flattenVisible(tree, new Set(["src/a"]))).toEqual(["src/b/two.ts", "root.ts"]);
  });

  it("drops everything under a collapsed ancestor", () => {
    expect(flattenVisible(tree, new Set(["src"]))).toEqual(["root.ts"]);
  });
});

describe("filterTree", () => {
  const tree = buildTree([file("src/a/one.ts"), file("src/b/two.tsx")]);

  it("keeps only files matching the substring, with their ancestors", () => {
    expect(flattenVisible(filterTree(tree, "one"), new Set())).toEqual(["src/a/one.ts"]);
  });

  it("matches on the full path, not just the file name", () => {
    expect(flattenVisible(filterTree(tree, "src/b"), new Set())).toEqual(["src/b/two.tsx"]);
  });

  it("is case-insensitive", () => {
    expect(flattenVisible(filterTree(tree, "ONE"), new Set())).toEqual(["src/a/one.ts"]);
  });

  it("returns the tree unchanged for an empty query", () => {
    expect(filterTree(tree, "")).toBe(tree);
  });

  it("returns nothing when nothing matches", () => {
    expect(filterTree(tree, "zzz")).toEqual([]);
  });
});

describe("ancestorDirs", () => {
  it("lists every directory prefix of a path", () => {
    expect(ancestorDirs("src/a/b/one.ts")).toEqual(["src", "src/a", "src/a/b"]);
  });

  it("returns nothing for a root-level file", () => {
    expect(ancestorDirs("one.ts")).toEqual([]);
  });
});
