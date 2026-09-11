import { render, screen } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";
import { http, HttpResponse, oneDoc, server } from "@/test/server";

/**
 * App owns a module-level QueryClient singleton (by design: one client for
 * the app's lifetime). useAuthMode's staleTime is Infinity, so once one
 * test's render has cached a mode, a later test in this same file would see
 * the stale cached value instead of its own mocked response. vi.resetModules
 * plus a fresh dynamic import gives each test its own App module instance —
 * and so its own QueryClient — without changing App's production shape.
 */
async function freshApp() {
  vi.resetModules();
  const mod = await import("@/App");
  return mod.App;
}

// sonner resolves a "system" theme prop via the same matchMedia stub the app's
// own ThemeProvider uses, so asserting on sonner's rendered DOM output cannot
// distinguish "we passed the resolved theme" from "we passed the literal
// string 'system' and sonner resolved it itself". Mock the module and capture
// the prop Toaster actually receives instead (FR-8.1).
const { capturedThemes } = vi.hoisted(() => ({ capturedThemes: [] as string[] }));

vi.mock("sonner", () => ({
  Toaster: (props: { theme?: string }) => {
    capturedThemes.push(props.theme ?? "");
    return null;
  },
  toast: vi.fn(),
}));

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));

beforeEach(() => {
  window.history.pushState({}, "", "/no-such-page");
  capturedThemes.length = 0;
  // Standalone is the default for these pre-existing tests: they assert the
  // app renders exactly as it did before ModeGate existed (FR-8.1).
  server.use(
    http.get("/api/auth/mode", () =>
      HttpResponse.json(
        oneDoc("modes", "current", { mode: "standalone", registrationOpen: false }),
      ),
    ),
  );
});

afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("App", () => {
  it("renders the shell chrome around the routed page", async () => {
    const App = await freshApp();
    render(<App />);
    expect(await screen.findByRole("link", { name: "Converge" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
    expect(screen.getByRole("main")).toContainElement(screen.getByText("Page not found"));
  });

  it("applies the stored theme on mount", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    const App = await freshApp();
    render(<App />);
    await screen.findByRole("link", { name: "Converge" });
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it.each([
    ["light", "light"],
    ["dark", "dark"],
    ["system", "light"],
  ] as const)(
    'passes Toaster the resolved theme for stored preference "%s", never the literal "system"',
    async (stored, expectedResolved) => {
      localStorage.setItem(THEME_STORAGE_KEY, stored);
      const App = await freshApp();
      render(<App />);
      await screen.findByRole("link", { name: "Converge" });
      expect(capturedThemes.length).toBeGreaterThan(0);
      const lastTheme = capturedThemes[capturedThemes.length - 1];
      expect(lastTheme).toBe(expectedResolved);
      expect(lastTheme).not.toBe("system");
    },
  );

  it("mounts no account menu and requests no /api/auth/me in standalone mode", async () => {
    let meRequested = false;
    server.use(
      http.get("/api/auth/me", () => {
        meRequested = true;
        return HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        );
      }),
    );
    const App = await freshApp();
    render(<App />);
    await screen.findByRole("link", { name: "Converge" });
    expect(screen.queryByRole("button", { name: "alice" })).not.toBeInTheDocument();
    expect(meRequested).toBe(false);
  });

  it("mounts the account menu in hosted mode once the user resolves", async () => {
    server.use(
      http.get("/api/auth/mode", () =>
        HttpResponse.json(oneDoc("modes", "current", { mode: "hosted", registrationOpen: true })),
      ),
      http.get("/api/auth/me", () =>
        HttpResponse.json(
          oneDoc("users", "u1", {
            username: "alice",
            createdAt: "2026-01-01T00:00:00Z",
            providerCount: 0,
          }),
        ),
      ),
    );
    const App = await freshApp();
    render(<App />);
    expect(await screen.findByRole("button", { name: "alice" })).toBeInTheDocument();
  });
});
