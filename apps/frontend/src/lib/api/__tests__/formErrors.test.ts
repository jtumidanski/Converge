import { describe, expect, it } from "vitest";
import { safeNext } from "@/lib/api/formErrors";

describe("safeNext", () => {
  it("returns / when next is absent", () => {
    expect(safeNext("")).toBe("/");
  });

  it("accepts a same-origin path with a query string", () => {
    expect(safeNext("?next=%2Fa%2Fb%3Fc%3Dd")).toBe("/a/b?c=d");
  });

  it("rejects a protocol-relative path as an open redirect", () => {
    expect(safeNext("?next=%2F%2Fevil.test")).toBe("/");
  });

  it("rejects an absolute external URL", () => {
    expect(safeNext("?next=https%3A%2F%2Fevil.test%2F")).toBe("/");
  });

  it("accepts the root path", () => {
    expect(safeNext("?next=%2F")).toBe("/");
  });

  it("rejects a backslash-based protocol-relative path", () => {
    expect(safeNext("?next=%2F%5Cevil.test")).toBe("/");
  });

  it("rejects a backslash-slash mixed protocol-relative path", () => {
    expect(safeNext("?next=%2F%5C%2Fevil.test")).toBe("/");
  });

  it("rejects a double-backslash protocol-relative path", () => {
    expect(safeNext("?next=%2F%5C%5Cevil.test")).toBe("/");
  });

  it("still accepts a legitimate same-origin path with a query string", () => {
    expect(safeNext("?next=%2Fa%2Fb%3Fc%3Dd")).toBe("/a/b?c=d");
  });
});
