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

  it("renders the optional right slot beside the theme control", () => {
    renderWithProviders(
      <AppShell right={<button type="button">account menu</button>}>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.getByRole("button", { name: "account menu" })).toBeInTheDocument();
  });

  it("renders no right slot content when none is passed", () => {
    renderWithProviders(
      <AppShell>
        <p>page content</p>
      </AppShell>,
    );
    expect(screen.queryByRole("button", { name: "account menu" })).not.toBeInTheDocument();
  });
});
