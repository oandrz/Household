// MemberService.Update (api/internal/usecase/member.go) revokes every one of
// the target's API tokens server-side on ANY successful role or capability
// change, not just a demotion -- see revokeCredentials. Without this hook
// also invalidating the household access list, the Access panel would keep
// showing a just-revoked token as live for up to householdAccessQueryKey's
// own staleTime (whole-branch review finding M2, 2026-09-23).
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useHouseholdAccess } from "./useHouseholdAccess";
import { useUpdateMember } from "./useUpdateMember";

const ACCESS_URL = "/api/v1/household/access";
const MEMBER_URL = "/api/v1/household/members/mem-1";
const EMPTY_ACCESS = { telegramEnabled: false, tokens: [], chats: [] };

function renderHooks() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderHook(() => ({ access: useHouseholdAccess(), update: useUpdateMember() }), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("useUpdateMember -- the access list refetches after a role/capability change", () => {
  it("invalidates householdAccessQueryKey so a revoked token stops reading as live", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      [`PATCH ${MEMBER_URL}`]: {
        status: 200,
        body: { id: "mem-1", role: "limited", capabilities: ["calendar"] },
      },
    });

    const { result } = renderHooks();
    await waitFor(() => expect(result.current.access.isSuccess).toBe(true));

    await act(async () => {
      await result.current.update.mutateAsync({ id: "mem-1", role: "limited", capabilities: ["calendar"] });
    });

    // One GET on mount, a second once the PATCH's own invalidation refetches it.
    expect(
      fetchMock.mock.calls.filter(
        ([input, init]) => String(input) === ACCESS_URL && (init?.method ?? "GET") === "GET",
      ),
    ).toHaveLength(2);
  });
});
