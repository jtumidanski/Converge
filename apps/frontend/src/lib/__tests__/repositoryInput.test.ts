import { describe, expect, it } from "vitest";
import { isValidRepositoryName, parseRepositoryInput } from "@/lib/repositoryInput";

const BASE = "https://gitlab.example.test";

describe("parseRepositoryInput", () => {
  it.each([
    ["atlas/server", "atlas/server"],
    ["  atlas/server  ", "atlas/server"],
    ["group/sub/project", "group/sub/project"],
  ])("accepts the plain name %s", (input, expected) => {
    expect(parseRepositoryInput(input, BASE)).toBe(expected);
  });

  it.each([
    [`${BASE}/atlas/server`, "atlas/server"],
    [`${BASE}/atlas/server/`, "atlas/server"],
    [`${BASE}/atlas/server.git`, "atlas/server"],
    [`${BASE}/group/sub/project`, "group/sub/project"],
  ])("accepts the provider URL %s", (input, expected) => {
    expect(parseRepositoryInput(input, BASE)).toBe(expected);
  });

  it.each([
    ["", "empty"],
    ["server", "no slash"],
    ["atlas server", "contains a space"],
    ["../etc/passwd", "traversal"],
    ["atlas/../etc", "traversal in a segment"],
    ["-atlas/server", "leading dash"],
    ["/atlas/server", "leading slash"],
    ["https://other.test/atlas/server", "a different origin"],
  ])("rejects %s (%s)", (input) => {
    expect(parseRepositoryInput(input, BASE)).toBeNull();
  });

  it("rejects a provider URL with no path", () => {
    expect(parseRepositoryInput(BASE, BASE)).toBeNull();
  });

  it("rejects any URL when the provider base URL is unknown", () => {
    expect(parseRepositoryInput(`${BASE}/atlas/server`, undefined)).toBeNull();
  });
});

describe("isValidRepositoryName", () => {
  it("mirrors the backend owner/name rule", () => {
    expect(isValidRepositoryName("atlas/server")).toBe(true);
    expect(isValidRepositoryName("atlas")).toBe(false);
    expect(isValidRepositoryName("atlas/.hidden")).toBe(false);
  });
});
