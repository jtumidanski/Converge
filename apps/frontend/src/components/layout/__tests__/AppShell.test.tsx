import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { renderWithProviders } from "@/test/render";

describe("AppShell", () => {
  it("renders its children", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByText("page content")).toBeInTheDocument();
  });

  it("renders the wordmark as a link home", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("link", { name: "Converge" })).toHaveAttribute("href", "/");
  });

  it("renders the theme control", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("button", { name: /change theme/i })).toBeInTheDocument();
  });

  it("puts the children in a main landmark below the banner", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(screen.getByRole("main")).toContainElement(screen.getByText("page content"));
  });
});
