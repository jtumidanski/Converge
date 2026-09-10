import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ThemeToggle } from "@/components/theme/ThemeToggle";
import { THEME_STORAGE_KEY } from "@/lib/theme/types";
import { renderWithProviders } from "@/test/render";
import { setSystemDark } from "@/test/matchMedia";

async function openMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /change theme/i }));
  await screen.findByRole("menu");
}

describe("ThemeToggle", () => {
  it("exposes an accessible trigger", () => {
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
  });

  it.each(["light", "dark", "system"] as const)(
    "names the current %s preference in the trigger's accessible name",
    (preference) => {
      localStorage.setItem(THEME_STORAGE_KEY, preference);
      renderWithProviders(<ThemeToggle />);
      expect(
        screen.getByRole("button", { name: `Change theme (currently ${preference})` }),
      ).toBeInTheDocument();
    },
  );

  it("names the preference, not the resolved theme, under System", () => {
    setSystemDark(true);
    localStorage.setItem(THEME_STORAGE_KEY, "system");
    renderWithProviders(<ThemeToggle />);
    expect(
      screen.getByRole("button", { name: "Change theme (currently system)" }),
    ).toBeInTheDocument();
  });

  it("offers exactly three modes", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    const items = screen.getAllByRole("menuitemradio");
    expect(items).toHaveLength(3);
    expect(items.map((item) => item.textContent)).toEqual(["Light", "Dark", "System"]);
  });

  it("marks the stored preference as checked", async () => {
    const user = userEvent.setup();
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    expect(screen.getByRole("menuitemradio", { name: "Dark", checked: true })).toBeInTheDocument();
    expect(
      screen.getByRole("menuitemradio", { name: "Light", checked: false }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitemradio", { name: "System", checked: false }),
    ).toBeInTheDocument();
  });

  it("keeps System checked while the trigger shows the resolved theme", async () => {
    const user = userEvent.setup();
    setSystemDark(true);
    renderWithProviders(<ThemeToggle />);

    // FR-6.5: the trigger reflects `resolved` (dark), so the moon is shown...
    expect(screen.getByRole("button", { name: /change theme/i })).toContainHTML("lucide-moon");

    await openMenu(user);

    // ...while FR-6.2 keeps the checked item on `preference` (system), not dark.
    expect(
      screen.getByRole("menuitemradio", { name: "System", checked: true }),
    ).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: "Dark", checked: false })).toBeInTheDocument();
  });

  it("shows the sun on the trigger when the resolved theme is light", () => {
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole("button", { name: /change theme/i })).toContainHTML("lucide-sun");
  });

  it("applies, persists, and closes the menu on selection", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    await user.click(screen.getByRole("menuitemradio", { name: "Dark" }));

    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());
  });

  it("persists the literal 'system' when System is selected", async () => {
    const user = userEvent.setup();
    setSystemDark(true);
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    await user.click(screen.getByRole("menuitemradio", { name: "System" }));

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("opens and selects by keyboard alone", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);

    await user.tab();
    expect(screen.getByRole("button", { name: /change theme/i })).toHaveFocus();

    await user.keyboard("{Enter}");
    await screen.findByRole("menu");
    // Opening via keyboard auto-highlights the first item (Light), so a single
    // ArrowDown moves the highlight to Dark before Enter selects it.
    await user.keyboard("{ArrowDown}{Enter}");

    await waitFor(() => expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark"));
  });

  it("dismisses with Escape without changing the theme", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ThemeToggle />);
    await openMenu(user);

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });
});
