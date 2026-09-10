import { vi } from "vitest";

const DARK_QUERY = "(prefers-color-scheme: dark)";

type ChangeListener = (event: MediaQueryListEvent) => void;

let systemIsDark = false;
const listeners = new Set<ChangeListener>();

const addEventListener = vi.fn((type: string, listener: ChangeListener) => {
  if (type === "change") listeners.add(listener);
});

const removeEventListener = vi.fn((type: string, listener: ChangeListener) => {
  if (type === "change") listeners.delete(listener);
});

/**
 * darkQueryListeners exposes the stub's subscription calls so a test can assert
 * that a listener was torn down (FR-5.3) without depending on call ordering.
 */
export const darkQueryListeners = {
  addEventListener,
  removeEventListener,
  count: () => listeners.size,
};

/** installMatchMedia defines a controllable window.matchMedia. jsdom has none. */
export function installMatchMedia(): void {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: vi.fn((query: string) => ({
      media: query,
      get matches() {
        return query === DARK_QUERY ? systemIsDark : false;
      },
      onchange: null,
      addEventListener,
      removeEventListener,
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    })),
  });
}

/** removeMatchMedia deletes the property entirely, for the FR-1.4 fallback test. */
export function removeMatchMedia(): void {
  Reflect.deleteProperty(window as unknown as Record<string, unknown>, "matchMedia");
}

/** setSystemDark flips the OS preference and notifies every live listener. */
export function setSystemDark(dark: boolean): void {
  systemIsDark = dark;
  const event = { matches: dark, media: DARK_QUERY } as MediaQueryListEvent;
  for (const listener of [...listeners]) listener(event);
}

/** resetMatchMedia returns the stub to "OS is light, nobody subscribed". */
export function resetMatchMedia(): void {
  systemIsDark = false;
  listeners.clear();
  addEventListener.mockClear();
  removeEventListener.mockClear();
}
