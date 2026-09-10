import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";

const html = readFileSync(path.resolve(import.meta.dirname, "../../index.html"), "utf8");

describe("pre-paint theme boot script in index.html", () => {
  it("reads the same storage key the TypeScript core uses", () => {
    expect(html).toContain(`localStorage.getItem("${THEME_STORAGE_KEY}")`);
  });

  it("validates the stored value against the three-mode allowlist", () => {
    expect(html).toContain('stored === "light" || stored === "dark" || stored === "system"');
  });

  it("writes the dark class and color-scheme onto the document root", () => {
    expect(html).toContain('classList.toggle("dark", dark)');
    expect(html).toContain('style.colorScheme = dark ? "dark" : "light"');
    expect(html).not.toContain("colorScheme = pref");
    expect(html).not.toContain('toggle("dark", pref');
  });

  it("runs before the application module script", () => {
    const bootIndex = html.indexOf("localStorage.getItem");
    const moduleIndex = html.indexOf('<script type="module"');
    expect(bootIndex).toBeGreaterThan(-1);
    expect(moduleIndex).toBeGreaterThan(-1);
    expect(bootIndex).toBeLessThan(moduleIndex);
  });

  it("is a classic script, not a deferred module", () => {
    const boot = html.slice(0, html.indexOf("localStorage.getItem"));
    const openingTag = boot.slice(boot.lastIndexOf("<script"));
    expect(openingTag).not.toContain("type=");
    expect(openingTag).not.toContain("defer");
    expect(openingTag).not.toContain("async");
  });

  it("guards the storage read, the OS query and the DOM write separately", () => {
    const boot = html.slice(html.indexOf("<script>"), html.indexOf("</script>"));
    // One coarse try/catch around the whole body would let a blocked
    // localStorage skip the OS-preference branch. See bootThemeScript.behavior
    // .test.ts for the executed proof; this is the structural guard.
    expect(boot.match(/try \{/g)).toHaveLength(3);
    expect(boot.match(/\} catch \{/g)).toHaveLength(3);
  });
});
