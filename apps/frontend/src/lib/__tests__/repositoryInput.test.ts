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
    // The backend (gitx.ValidateRepoFullName) only rejects a leading `.`, `-`,
    // or `/` on the whole string, plus `.`/`..`/`..`-containing segments — not
    // any segment merely starting with `.` or `-`. `org/.github` is a real
    // GitHub repository the backend accepts, so the client must too.
    expect(isValidRepositoryName("atlas/.hidden")).toBe(true);
    expect(isValidRepositoryName("org/.github")).toBe(true);
  });

  it.each([
    ["..", "a bare double-dot"],
    ["a/../b", "a double-dot segment"],
    ["a/..b/c", "a segment containing .. without being exactly .."],
    ["/atlas/server", "a leading slash"],
    ["-atlas/server", "a leading dash"],
    [".atlas/server", "a leading dot on the whole string"],
    ["atlas//server", "an empty segment"],
  ])("rejects %s (%s)", (input) => {
    expect(isValidRepositoryName(input)).toBe(false);
  });
});
