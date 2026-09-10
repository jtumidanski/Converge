import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { ManualRepositoryForm } from "@/components/features/repositories/ManualRepositoryForm";

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("ManualRepositoryForm", () => {
  it("rejects malformed repository names without calling the API", async () => {
    const onResolved = vi.fn();
    let calls = 0;
    server.use(
      http.get("/api/providers/gh/repositories/:repo", () => {
        calls += 1;
        return HttpResponse.json({ errors: [] }, { status: 404 });
      }),
    );
    renderWithProviders(<ManualRepositoryForm providerId="gh" onResolved={onResolved} />);
    await userEvent.type(screen.getByLabelText(/repository/i), "not-a-repo");
    await userEvent.click(screen.getByRole("button", { name: /use repository/i }));
    expect(await screen.findByText(/owner\/name/i)).toBeInTheDocument();
    expect(calls).toBe(0);
    expect(onResolved).not.toHaveBeenCalled();
  });

  it("validates against the API and reports the resolved repository", async () => {
    const onResolved = vi.fn();
    server.use(
      http.get("/api/providers/gh/repositories/:repo", ({ params }) => {
        expect(params.repo).toBe("atlas/server");
        return HttpResponse.json(
          oneDoc("repositories", "atlas/server", {
            name: "server",
            namespace: "atlas",
            defaultBranch: "main",
            webUrl: "u",
          }),
        );
      }),
    );
    renderWithProviders(<ManualRepositoryForm providerId="gh" onResolved={onResolved} />);
    await userEvent.type(screen.getByLabelText(/repository/i), "atlas/server");
    await userEvent.click(screen.getByRole("button", { name: /use repository/i }));
    await waitFor(() => expect(onResolved).toHaveBeenCalledTimes(1));
    expect(onResolved.mock.calls[0]?.[0]?.id).toBe("atlas/server");
  });

  it("shows the API message when the repository is not found", async () => {
    server.use(
      http.get("/api/providers/gh/repositories/:repo", () =>
        HttpResponse.json(
          {
            errors: [
              {
                status: "404",
                code: "NOT_FOUND",
                title: "Not Found",
                detail: "The requested resource does not exist.",
              },
            ],
          },
          { status: 404 },
        ),
      ),
    );
    renderWithProviders(<ManualRepositoryForm providerId="gh" onResolved={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/repository/i), "atlas/missing");
    await userEvent.click(screen.getByRole("button", { name: /use repository/i }));
    expect(await screen.findByText(/does not exist/i)).toBeInTheDocument();
  });
});
