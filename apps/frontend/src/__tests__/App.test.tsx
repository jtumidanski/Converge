import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { toast } from "sonner";
import { App } from "@/App";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";

beforeEach(() => {
  window.history.pushState({}, "", "/no-such-page");
});

describe("App", () => {
  it("renders the shell chrome around the routed page", () => {
    render(<App />);
    expect(screen.getByRole("link", { name: "Converge" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
    expect(screen.getByRole("main")).toContainElement(screen.getByText("Page not found"));
  });

  it("applies the stored theme on mount", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    render(<App />);
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("gives the toaster the resolved theme, never the literal system", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "system");
    render(<App />);
    // sonner only renders its themed container once a toast is queued, so
    // trigger one to surface the "data-sonner-theme" attribute it sets.
    act(() => {
      toast("hello");
    });
    await waitFor(() => {
      expect(document.querySelector("[data-sonner-toaster]")).not.toBeNull();
    });
    const toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster?.getAttribute("data-sonner-theme")).toBe("light");
  });
});
