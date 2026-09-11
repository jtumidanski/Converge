import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";

function Publisher({ label }: { label: string }) {
  useBreadcrumbs([{ label: "Reviews", to: "/" }, { label }]);
  return <p>page body</p>;
}

function renderShell(children: React.ReactNode) {
  return render(
    <ThemeProvider>
      <MemoryRouter>
        <AppShell>{children}</AppShell>
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("AppShell", () => {
  it("renders the brand block as a link to the root", () => {
    renderShell(<p>body</p>);
    const brand = screen.getByRole("link", { name: /converge/i });
    expect(brand).toHaveAttribute("href", "/");
  });

  it("keeps the theme toggle in the top bar", () => {
    renderShell(<p>body</p>);
    expect(screen.getByRole("button", { name: /theme/i })).toBeInTheDocument();
  });

  it("shows Reviews as the default breadcrumb when no page publishes one", () => {
    renderShell(<p>body</p>);
    expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toHaveTextContent("Reviews");
  });

  it("renders the segments a page publishes", () => {
    renderShell(<Publisher label="atlas/server" />);
    const nav = screen.getByRole("navigation", { name: /breadcrumb/i });
    expect(nav).toHaveTextContent("Reviews");
    expect(nav).toHaveTextContent("atlas/server");
    expect(screen.getAllByRole("link", { name: "Reviews" })[0]).toHaveAttribute("href", "/");
  });

  it("falls back to the default once the publishing page unmounts", () => {
    const { rerender } = renderShell(<Publisher label="atlas/server" />);
    rerender(
      <ThemeProvider>
        <MemoryRouter>
          <AppShell>
            <p>body</p>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>,
    );
    expect(screen.getByRole("navigation", { name: /breadcrumb/i })).not.toHaveTextContent(
      "atlas/server",
    );
  });

  it("puts page content in the one shared centred container", () => {
    renderShell(<p>body</p>);
    const main = screen.getByRole("main");
    expect(main.className).toContain("mx-auto");
    expect(main.className).toContain("max-w-[80rem]");
    expect(main.className).toContain("px-6");
  });
});
