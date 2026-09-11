import { screen } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { ModeGate } from "@/components/auth/ModeGate";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderGate() {
  return renderWithProviders(
    <ModeGate>
      <div>child content</div>
    </ModeGate>,
  );
}

describe("ModeGate", () => {
  it("renders the standalone tree with no auth provider and no /api/auth/me request", async () => {
    let meRequested = false;
    server.use(
      http.get("/api/auth/mode", () =>
        HttpResponse.json(
          oneDoc("modes", "current", { mode: "standalone", registrationOpen: false }),
        ),
      ),
      http.get("/api/auth/me", () => {
        meRequested = true;
        return HttpResponse.json(errorDoc(401, "UNAUTHENTICATED", "Not signed in"), {
          status: 401,
        });
      }),
    );
    renderGate();
    expect(await screen.findByText("child content")).toBeInTheDocument();
    expect(meRequested).toBe(false);
  });

  it("renders the hosted tree, mounting an auth-aware provider", async () => {
    server.use(
      http.get("/api/auth/mode", () =>
        HttpResponse.json(oneDoc("modes", "current", { mode: "hosted", registrationOpen: true })),
      ),
    );
    renderGate();
    expect(await screen.findByText("child content")).toBeInTheDocument();
  });

  it("shows a loading state before the mode resolves and never flashes standalone content early", () => {
    server.use(
      http.get("/api/auth/mode", async () => {
        await new Promise(() => {
          // Never resolves within this test: assert the pre-resolution state.
        });
        return HttpResponse.json(oneDoc("modes", "current", { mode: "standalone" }));
      }),
    );
    const { container } = renderGate();
    expect(screen.queryByText("child content")).not.toBeInTheDocument();
    expect(container.querySelector('[aria-busy="true"]')).toBeInTheDocument();
  });

  it("surfaces a mode fetch failure as an error banner, not a blank page", async () => {
    server.use(http.get("/api/auth/mode", () => new HttpResponse(null, { status: 500 })));
    renderGate();
    expect(
      await screen.findByText("Converge could not determine how this instance is configured."),
    ).toBeInTheDocument();
    expect(screen.queryByText("child content")).not.toBeInTheDocument();
  });
});
