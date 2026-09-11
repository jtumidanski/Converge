import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { BaseBranchSelect } from "@/components/features/changes/BaseBranchSelect";
import { HttpResponse, http, listDoc, server } from "@/test/server";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function branch(name: string, isDefault = false) {
  return {
    type: "branches" as const,
    id: `gl:atlas/server:${name}`,
    attributes: { name, isDefault, sha: "" },
  };
}

function renderSelect(
  onChange = vi.fn(),
  overrides: { value?: string; defaultBranch?: string } = {},
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  render(
    <QueryClientProvider client={client}>
      <BaseBranchSelect
        providerId="gl"
        repository="atlas/server"
        value={overrides.value ?? "main"}
        defaultBranch={overrides.defaultBranch ?? "main"}
        onChange={onChange}
      />
    </QueryClientProvider>,
  );
  return onChange;
}

describe("BaseBranchSelect", () => {
  it("shows the current base on the trigger", () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([branch("main", true), branch("develop")])),
      ),
    );
    renderSelect(vi.fn(), { value: "develop", defaultBranch: "main" });
    expect(screen.getByRole("combobox", { name: /base/i })).toHaveTextContent("develop");
  });

  it("pins the repository default at the top even when the server did not", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([branch("develop"), branch("main", true)])),
      ),
    );
    renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    const options = await screen.findAllByRole("option");
    expect(options[0]).toHaveTextContent("main");
  });

  it("sends a debounced search to the branches endpoint", async () => {
    const seen: string[] = [];
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", ({ request }) => {
        seen.push(new URL(request.url).searchParams.get("search") ?? "");
        return HttpResponse.json(listDoc([branch("release/1.0")]));
      }),
    );
    renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.type(screen.getByPlaceholderText(/filter branches/i), "rel");
    await waitFor(() => expect(seen).toContain("rel"));
  });

  it("selects a branch and reports it", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([branch("main", true), branch("develop")])),
      ),
    );
    const onChange = renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.click(await screen.findByRole("option", { name: /develop/ }));
    expect(onChange).toHaveBeenCalledWith("develop");
  });

  it("lets a typed branch beyond the loaded page be used as-is", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([])),
      ),
    );
    const onChange = renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.type(screen.getByPlaceholderText(/filter branches/i), "feature/x");
    await userEvent.click(await screen.findByRole("option", { name: /use “feature\/x”/i }));
    expect(onChange).toHaveBeenCalledWith("feature/x");
  });
});
