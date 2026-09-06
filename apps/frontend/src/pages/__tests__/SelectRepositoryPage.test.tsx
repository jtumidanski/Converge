import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate };
});

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
});
afterAll(() => server.close());

function seedProviders() {
  server.use(
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          oneDoc("providers", "gitlab-work", {
            displayName: "GitLab Work",
            kind: "gitlab",
            baseUrl: "https://gitlab.test",
          }).data,
        ]),
      ),
    ),
  );
}

describe("SelectRepositoryPage", () => {
  it("shows a skeleton, then repositories for the only provider", async () => {
    seedProviders();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          listDoc(
            [
              oneDoc("repositories", "atlas/server", {
                name: "server",
                namespace: "atlas",
                defaultBranch: "main",
                webUrl: "u",
              }).data,
            ],
            { number: 1, size: 30, hasNext: false },
          ),
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText("main")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /select/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalled());
    expect(String(navigate.mock.calls[0]?.[0])).toContain("provider=gitlab-work");
    expect(String(navigate.mock.calls[0]?.[0])).toContain("repo=atlas%2Fserver");
  });

  it("surfaces provider failures in an error banner", async () => {
    server.use(
      http.get("/api/providers", () =>
        HttpResponse.json(
          {
            errors: [
              {
                status: "502",
                code: "PROVIDER_AUTH",
                title: "Bad Gateway",
                detail: "The token was rejected.",
              },
            ],
          },
          { status: 502 },
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/token was rejected/i)).toBeInTheDocument();
  });

  it("renders an empty state when no repositories come back", async () => {
    seedProviders();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(listDoc([], { number: 1, size: 30, hasNext: false })),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/no repositories/i)).toBeInTheDocument();
  });

  it("surfaces repository failures in an error banner rather than an empty state", async () => {
    seedProviders();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          {
            errors: [
              {
                status: "REPOSITORY_UNAVAILABLE",
                code: "REPOSITORY_UNAVAILABLE",
                title: "Repository Unavailable",
                detail: "The repository host did not respond.",
              },
            ],
          },
          { status: 503 },
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/repository host did not respond/i)).toBeInTheDocument();
    expect(screen.queryByText(/no repositories/i)).not.toBeInTheDocument();
  });

  // A disabled React Query reports isLoading=false with data=undefined, so a
  // component that only checks isLoading renders its empty state having never
  // issued a request. Until a provider is chosen the repositories query is
  // disabled, and "This token cannot see any repositories" is a false claim.
  it("does not claim the token sees no repositories before a provider is known", () => {
    seedProviders();
    renderWithProviders(<SelectRepositoryPage />);
    expect(screen.queryByText(/cannot see any repositories/i)).not.toBeInTheDocument();
  });

  it("does not claim the token sees no repositories when no provider is configured", async () => {
    server.use(http.get("/api/providers", () => HttpResponse.json(listDoc([]))));
    renderWithProviders(<SelectRepositoryPage />);
    // The picker's label replaces its skeleton once the providers query
    // settles, so this waits for the steady state rather than a first paint.
    expect(await screen.findByText("Provider")).toBeInTheDocument();
    expect(screen.queryByText(/cannot see any repositories/i)).not.toBeInTheDocument();
  });

  it("retries the repository fetch when Try again is pressed after a failure", async () => {
    seedProviders();
    let calls = 0;
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () => {
        calls += 1;
        if (calls === 1) {
          return HttpResponse.json(
            {
              errors: [
                {
                  status: "503",
                  code: "REPOSITORY_UNAVAILABLE",
                  title: "Repository Unavailable",
                  detail: "The repository host did not respond.",
                },
              ],
            },
            { status: 503 },
          );
        }
        return HttpResponse.json(
          listDoc(
            [
              oneDoc("repositories", "atlas/server", {
                name: "server",
                namespace: "atlas",
                defaultBranch: "main",
                webUrl: "u",
              }).data,
            ],
            { number: 1, size: 30, hasNext: false },
          ),
        );
      }),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/repository host did not respond/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    expect(calls).toBe(2);
  });
});
