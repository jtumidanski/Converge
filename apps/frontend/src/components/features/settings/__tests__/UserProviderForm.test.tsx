import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { errorDoc, http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { UserProviderForm } from "@/components/features/settings/UserProviderForm";
import type { UserProvider } from "@/types/models/auth";

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function providerFixture(overrides: Partial<UserProvider> = {}): UserProvider {
  return {
    id: "p1",
    slug: "gh-main",
    displayName: "Main GitHub",
    kind: "github",
    baseUrl: "",
    tokenLast4: "9f2c",
    tokenSetAt: "2026-01-01T00:00:00Z",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("UserProviderForm (create)", () => {
  it("creates a provider", async () => {
    const onSubmitted = vi.fn();
    let body: unknown;
    server.use(
      http.post("/api/settings/providers", async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(
          oneDoc("userProviders", "p1", {
            slug: "gh-main",
            displayName: "Main GitHub",
            kind: "github",
            baseUrl: "",
            tokenLast4: "9f2c",
            tokenSetAt: "2026-01-01T00:00:00Z",
            createdAt: "2026-01-01T00:00:00Z",
            updatedAt: "2026-01-01T00:00:00Z",
          }),
        );
      }),
    );
    renderWithProviders(
      <UserProviderForm mode="create" onSubmitted={onSubmitted} onCancel={vi.fn()} />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Slug"), "gh-main");
    await user.type(screen.getByLabelText("Display name"), "Main GitHub");
    await user.type(screen.getByLabelText("Access token"), "ghp_secret");
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    await waitFor(() => expect(onSubmitted).toHaveBeenCalled());
    expect(body).toEqual({
      data: {
        type: "userProviders",
        attributes: {
          slug: "gh-main",
          displayName: "Main GitHub",
          kind: "github",
          baseUrl: "",
          token: "ghp_secret",
          validate: true,
        },
      },
    });
  });

  it("does not require a base URL for github", async () => {
    let body: { data: { attributes: { baseUrl: string } } } | undefined;
    server.use(
      http.post("/api/settings/providers", async ({ request }) => {
        body = (await request.json()) as typeof body;
        return HttpResponse.json(oneDoc("userProviders", "p1", providerFixture()));
      }),
    );
    renderWithProviders(
      <UserProviderForm mode="create" onSubmitted={vi.fn()} onCancel={vi.fn()} />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Slug"), "gh-main");
    await user.type(screen.getByLabelText("Display name"), "Main GitHub");
    await user.type(screen.getByLabelText("Access token"), "ghp_secret");
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    await waitFor(() => expect(body).toBeDefined());
    expect(body?.data.attributes.baseUrl).toBe("");
  });

  it("surfaces PROVIDER_UNAUTHORIZED on create on the token field", async () => {
    server.use(
      http.post("/api/settings/providers", () =>
        HttpResponse.json(errorDoc(422, "PROVIDER_UNAUTHORIZED", "That token was rejected."), {
          status: 422,
        }),
      ),
    );
    renderWithProviders(
      <UserProviderForm mode="create" onSubmitted={vi.fn()} onCancel={vi.fn()} />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Slug"), "gh-main");
    await user.type(screen.getByLabelText("Display name"), "Main GitHub");
    await user.type(screen.getByLabelText("Access token"), "ghp_bad");
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    expect(await screen.findByText("That token was rejected.")).toBeInTheDocument();
    expect(screen.getByLabelText("Access token")).toHaveAttribute("aria-invalid", "true");
  });

  it("surfaces PROVIDER_SLUG_TAKEN on the slug field", async () => {
    server.use(
      http.post("/api/settings/providers", () =>
        HttpResponse.json(errorDoc(409, "PROVIDER_SLUG_TAKEN", "That slug is already in use."), {
          status: 409,
        }),
      ),
    );
    renderWithProviders(
      <UserProviderForm mode="create" onSubmitted={vi.fn()} onCancel={vi.fn()} />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Slug"), "gh-main");
    await user.type(screen.getByLabelText("Display name"), "Main GitHub");
    await user.type(screen.getByLabelText("Access token"), "ghp_secret");
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    expect(await screen.findByText("That slug is already in use.")).toBeInTheDocument();
    expect(screen.getByLabelText("Slug")).toHaveAttribute("aria-invalid", "true");
  });
});

describe("UserProviderForm (edit)", () => {
  it("hides the slug field when editing", () => {
    renderWithProviders(
      <UserProviderForm
        mode="edit"
        provider={providerFixture()}
        onSubmitted={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText("Slug")).not.toBeInTheDocument();
    expect(screen.getByText("gh-main")).toBeInTheDocument();
  });

  it("loads the token input empty with a keep-current placeholder", () => {
    renderWithProviders(
      <UserProviderForm
        mode="edit"
        provider={providerFixture()}
        onSubmitted={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    const token = screen.getByLabelText("Access token") as HTMLInputElement;
    expect(token.value).toBe("");
    expect(token).toHaveAttribute("placeholder", "Leave blank to keep the current token");
  });

  it("edits a provider without touching the token, sending no token key", async () => {
    const onSubmitted = vi.fn();
    let body: { data: { attributes: Record<string, unknown> } } | undefined;
    server.use(
      http.patch("/api/settings/providers/p1", async ({ request }) => {
        body = (await request.json()) as typeof body;
        return HttpResponse.json(oneDoc("userProviders", "p1", providerFixture()));
      }),
    );
    renderWithProviders(
      <UserProviderForm
        mode="edit"
        provider={providerFixture()}
        onSubmitted={onSubmitted}
        onCancel={vi.fn()}
      />,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(onSubmitted).toHaveBeenCalled());
    expect(body?.data.attributes).not.toHaveProperty("token");
  });

  it("edits a provider and rotates the token", async () => {
    let body: { data: { attributes: Record<string, unknown> } } | undefined;
    server.use(
      http.patch("/api/settings/providers/p1", async ({ request }) => {
        body = (await request.json()) as typeof body;
        return HttpResponse.json(oneDoc("userProviders", "p1", providerFixture()));
      }),
    );
    renderWithProviders(
      <UserProviderForm
        mode="edit"
        provider={providerFixture()}
        onSubmitted={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Access token"), "ghp_rotated");
    await user.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(body?.data.attributes.token).toBe("ghp_rotated"));
  });
});

// The kind field is the shipped Radix Select rather than a native <select>, so
// it is a button with role "combobox" whose label reflects the form's current
// value. These assert the control is wired to form state in both variants: the
// create default, and the value an existing provider arrives with.
describe("UserProviderForm kind field", () => {
  it("renders the shipped Select showing the create default", () => {
    renderWithProviders(
      <UserProviderForm mode="create" onSubmitted={vi.fn()} onCancel={vi.fn()} />,
    );
    expect(screen.getByRole("combobox", { name: "Kind" })).toHaveTextContent("GitHub");
  });

  it("renders the edited provider's kind", () => {
    renderWithProviders(
      <UserProviderForm
        mode="edit"
        provider={providerFixture({ kind: "gitlab", baseUrl: "https://gitlab.example.com" })}
        onSubmitted={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByRole("combobox", { name: "Kind" })).toHaveTextContent("GitLab");
  });
});
