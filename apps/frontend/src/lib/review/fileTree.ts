import type { ReviewFile } from "@/types/models/reviewFile";

export type TreeNode =
  | { kind: "dir"; name: string; path: string; children: TreeNode[] }
  | { kind: "file"; file: ReviewFile };

interface MutableDir {
  name: string;
  path: string;
  dirs: Map<string, MutableDir>;
  files: ReviewFile[];
}

function emptyDir(name: string, path: string): MutableDir {
  return { name, path, dirs: new Map(), files: [] };
}

/**
 * buildTree turns a flat file list into a directory tree, then collapses any
 * directory whose only child is a directory into a single `a/b/c` row. Long
 * Java-style package paths are the reason this exists: without collapsing,
 * the tree is mostly one-child rungs and the file names are pushed off-screen.
 */
export function buildTree(files: ReviewFile[]): TreeNode[] {
  const root = emptyDir("", "");
  for (const file of files) {
    const segments = file.attributes.path.split("/");
    const fileName = segments.pop();
    if (fileName === undefined) continue;
    let cursor = root;
    for (const segment of segments) {
      const path = cursor.path === "" ? segment : `${cursor.path}/${segment}`;
      let next = cursor.dirs.get(segment);
      if (!next) {
        next = emptyDir(segment, path);
        cursor.dirs.set(segment, next);
      }
      cursor = next;
    }
    cursor.files.push(file);
  }
  return toNodes(root);
}

function toNodes(dir: MutableDir): TreeNode[] {
  const dirs = [...dir.dirs.values()].sort((a, b) => a.name.localeCompare(b.name)).map(collapse);
  const files: TreeNode[] = [...dir.files]
    .sort((a, b) => a.attributes.path.localeCompare(b.attributes.path))
    .map((file) => ({ kind: "file", file }) as const);
  return [...dirs, ...files];
}

function collapse(dir: MutableDir): TreeNode {
  let current = dir;
  let name = dir.name;
  // Only a directory holding exactly one directory and no files can fold into
  // its child; anything else would hide a sibling.
  while (current.files.length === 0 && current.dirs.size === 1) {
    const [only] = current.dirs.values();
    if (!only) break;
    name = `${name}/${only.name}`;
    current = only;
  }
  return { kind: "dir", name, path: current.path, children: toNodes(current) };
}

/**
 * flattenVisible is the file order j/k, the footer's Next file, and "File i of
 * n" all use: depth-first tree order, skipping anything under a collapsed
 * directory.
 */
export function flattenVisible(nodes: TreeNode[], collapsed: ReadonlySet<string>): string[] {
  const out: string[] = [];
  const walk = (list: TreeNode[]): void => {
    for (const node of list) {
      if (node.kind === "file") {
        out.push(node.file.attributes.path);
        continue;
      }
      if (collapsed.has(node.path)) continue;
      walk(node.children);
    }
  };
  walk(nodes);
  return out;
}

/**
 * filterTree keeps files whose full path contains query (case-insensitively)
 * along with the directories leading to them. An empty query returns the same
 * array identity so callers can memoise on it.
 */
export function filterTree(nodes: TreeNode[], query: string): TreeNode[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return nodes;
  const keep = (list: TreeNode[]): TreeNode[] => {
    const out: TreeNode[] = [];
    for (const node of list) {
      if (node.kind === "file") {
        if (node.file.attributes.path.toLowerCase().includes(needle)) out.push(node);
        continue;
      }
      const children = keep(node.children);
      if (children.length > 0) out.push({ ...node, children });
    }
    return out;
  };
  return keep(nodes);
}

/** ancestorDirs lists every directory prefix of a file path, outermost first. */
export function ancestorDirs(path: string): string[] {
  const segments = path.split("/");
  segments.pop();
  const out: string[] = [];
  let prefix = "";
  for (const segment of segments) {
    prefix = prefix === "" ? segment : `${prefix}/${segment}`;
    out.push(prefix);
  }
  return out;
}
