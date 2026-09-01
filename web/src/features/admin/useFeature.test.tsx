import { describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useFeature } from "./useFeature";
import { meQueryKey } from "../auth/useAuth";

function wrapperWithFeatures(features: Record<string, boolean>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(meQueryKey, {
    user: { id: "u1", email: "a@b.c", displayName: "A", avatarInitial: "A" },
    household: {
      id: "h1", name: "H", familyName: "H", primaryCurrency: "SGD",
      showSecondaryCurrency: false, secondaryCurrency: "", fxRateMode: "manual",
    },
    membership: { id: "m1", householdId: "h1", userId: "u1", role: "owner", capabilities: [] },
    capabilities: [],
    spaces: [],
    isPlatformAdmin: false,
    features,
  });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useFeature", () => {
  it("is true when the flag is on", async () => {
    const { result } = renderHook(() => useFeature("family_calendar"), {
      wrapper: wrapperWithFeatures({ family_calendar: true }),
    });
    await waitFor(() => expect(result.current).toBe(true));
  });

  // An unknown key must close a door, never open one -- the same fail-closed
  // rule the server's FlagSet.Enabled follows.
  it("is false for a key the server did not send", async () => {
    const { result } = renderHook(() => useFeature("typo"), {
      wrapper: wrapperWithFeatures({ family_calendar: true }),
    });
    await waitFor(() => expect(result.current).toBe(false));
  });

  it("is false before /auth/me has answered", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useFeature("family_calendar"), { wrapper });
    expect(result.current).toBe(false);
  });
});
