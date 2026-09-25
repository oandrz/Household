import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { ApiTokenList } from "./ApiTokenList";
import type { AccessToken } from "./schemas";

function token(overrides: Partial<AccessToken>): AccessToken {
  return {
    id: "t-1",
    memberId: "u-me",
    memberName: "Andreas",
    name: "laptop",
    prefix: "ab12cd34",
    createdAt: "2026-09-01T10:00:00Z",
    expiresAt: "2026-12-01T10:00:00Z",
    lastUsedAt: null,
    ...overrides,
  };
}

function renderList(tokens: AccessToken[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ApiTokenList tokens={tokens} myUserId="u-me" />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("ApiTokenList", () => {
  it("offers Revoke on my own tokens only", () => {
    stubFetchRoutes({});
    renderList([
      token({ id: "t-mine", name: "my laptop" }),
      token({ id: "t-partner", name: "partner script", memberId: "u-partner", memberName: "Christine" }),
    ]);

    expect(screen.getByText("my laptop")).toBeInTheDocument();
    expect(screen.getByText("partner script")).toBeInTheDocument();
    expect(screen.getByText(/Christine/)).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Revoke" })).toHaveLength(1);
  });

  it("revokes my token after a confirm, with a DELETE to its id", async () => {
    const fetchMock = stubFetchRoutes({
      "DELETE /api/v1/auth/tokens/t-mine": { status: 204, body: undefined },
    });
    renderList([token({ id: "t-mine", name: "my laptop" })]);

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(await screen.findByRole("button", { name: "Yes, revoke" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === "/api/v1/auth/tokens/t-mine" && (init?.method ?? "").toUpperCase() === "DELETE",
      );
      expect(call).toBeDefined();
    });
  });

  it("says so when there are no tokens", () => {
    stubFetchRoutes({});
    renderList([]);
    expect(screen.getByText("No API tokens.")).toBeInTheDocument();
  });

  it("shows never-used and the prefix, never a secret", () => {
    stubFetchRoutes({});
    renderList([token({})]);
    expect(screen.getByText(/Never used/)).toBeInTheDocument();
    expect(screen.getByText(/ab12cd34/)).toBeInTheDocument();
  });
});
