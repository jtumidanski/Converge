import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";
import { installMatchMedia, resetMatchMedia } from "./matchMedia";

/**
 * jsdom implements neither matchMedia (theme resolution) nor ResizeObserver
 * (Radix's Popper, which backs dropdown-menu). Install both for every test.
 */
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeEach(() => {
  installMatchMedia();
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  // jsdom does not implement scrollIntoView either; cmdk (the new-review
  // drawer's command palette) calls it on every highlighted-row change.
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  cleanup();
  resetMatchMedia();
  localStorage.clear();
  // jsdom shares one document per file; a test that went dark must not leak.
  document.documentElement.className = "";
  document.documentElement.style.colorScheme = "";
});
