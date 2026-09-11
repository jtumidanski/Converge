import { describe, expect, it } from "vitest";
import { ticketKey } from "@/lib/changes/ticketKey";
import { isDependencyBot } from "@/lib/changes/dependencyBot";
import { applyOrder } from "@/lib/changes/applyOrder";
import { groupByTicket, NO_TICKET_LABEL } from "@/lib/changes/groupByTicket";
import { distinctAuthors } from "@/lib/changes/authors";
import type { Change } from "@/types/models/change";

function change(overrides: Partial<Change["attributes"]> & { number: number }): Change {
  return {
    type: "changes",
    id: String(overrides.number),
    attributes: {
      title: "Some change",
      author: "jsmith",
      sourceBranch: "feat/x",
      targetBranch: "main",
      mergedAt: "2026-01-01T00:00:00Z",
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: "https://example.test/1",
      ...overrides,
    },
  };
}

describe("ticketKey", () => {
  it.each([
    ["ATLAS-421 add a thing", "ATLAS-421"],
    ["fix(api): ATLAS-1 tidy up", "ATLAS-1"],
    ["ATLAS-1 and PROJ-2 together", "ATLAS-1"],
    ["AB1-99 mixed alphanumeric", "AB1-99"],
  ])("extracts %s", (title, expected) => {
    expect(ticketKey(title)).toBe(expected);
  });

  it.each([
    ["no ticket here"],
    ["lowercase-42 is not a key"],
    ["A-1 needs two leading letters"],
    ["ATLAS- missing the number"],
    ["ATLAS-x not a number"],
  ])("returns null for %s", (title) => {
    expect(ticketKey(title)).toBeNull();
  });
});

describe("isDependencyBot", () => {
  it.each([
    ["renovate/lodash-4.x", true],
    ["Renovate/Lodash", true],
    ["dependabot/npm_and_yarn/vite", true],
    ["DEPENDABOT/pip/x", true],
    ["feat/renovate-config", false],
    ["chore/deps", false],
    ["", false],
  ])("%s -> %s", (sourceBranch, expected) => {
    expect(isDependencyBot(change({ number: 1, sourceBranch }))).toBe(expected);
  });
});

describe("applyOrder", () => {
  it("sorts by merge time ascending", () => {
    const ordered = applyOrder([
      change({ number: 3, mergedAt: "2026-03-01T00:00:00Z" }),
      change({ number: 1, mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 2, mergedAt: "2026-02-01T00:00:00Z" }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([1, 2, 3]);
  });

  it("breaks ties on merge time by ascending number", () => {
    const ordered = applyOrder([
      change({ number: 9, mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 4, mergedAt: "2026-01-01T00:00:00Z" }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([4, 9]);
  });

  it("sorts a null merge time last", () => {
    const ordered = applyOrder([
      change({ number: 1, mergedAt: null }),
      change({ number: 2, mergedAt: "2026-01-01T00:00:00Z" }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([2, 1]);
  });

  it("sorts two null merge times by number", () => {
    const ordered = applyOrder([
      change({ number: 5, mergedAt: null }),
      change({ number: 2, mergedAt: null }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([2, 5]);
  });

  it("does not mutate its input and accepts any iterable", () => {
    const input = [change({ number: 2 }), change({ number: 1 })];
    const map = new Map(input.map((c) => [c.attributes.number, c]));
    expect(applyOrder(map.values()).map((c) => c.attributes.number)).toEqual([1, 2]);
    expect(input.map((c) => c.attributes.number)).toEqual([2, 1]);
  });
});

describe("groupByTicket", () => {
  it("orders groups by their newest change and puts No ticket last", () => {
    const groups = groupByTicket([
      change({ number: 1, title: "ATLAS-1 old", mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 2, title: "untagged", mergedAt: "2026-05-01T00:00:00Z" }),
      change({ number: 3, title: "PROJ-9 newer", mergedAt: "2026-03-01T00:00:00Z" }),
    ]);
    expect(groups.map((g) => g.label)).toEqual(["PROJ-9", "ATLAS-1", NO_TICKET_LABEL]);
    expect(groups.at(-1)?.key).toBeNull();
  });

  it("keeps server order within a group", () => {
    const groups = groupByTicket([
      change({ number: 7, title: "ATLAS-1 b", mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 3, title: "ATLAS-1 a", mergedAt: "2026-02-01T00:00:00Z" }),
    ]);
    expect(groups[0]?.changes.map((c) => c.attributes.number)).toEqual([7, 3]);
  });

  it("returns no groups for no changes", () => {
    expect(groupByTicket([])).toEqual([]);
  });

  it("omits the No ticket group when every change has a key", () => {
    const groups = groupByTicket([change({ number: 1, title: "ATLAS-1 x" })]);
    expect(groups).toHaveLength(1);
  });
});

describe("distinctAuthors", () => {
  it("returns each author once, sorted", () => {
    expect(
      distinctAuthors([
        change({ number: 1, author: "zoe" }),
        change({ number: 2, author: "adam" }),
        change({ number: 3, author: "zoe" }),
      ]),
    ).toEqual(["adam", "zoe"]);
  });

  it("skips empty authors", () => {
    expect(distinctAuthors([change({ number: 1, author: "" })])).toEqual([]);
  });
});
