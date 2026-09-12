// What a write invalidates, which is a decision rather than a detail: a figure
// that does not refetch is a screen showing yesterday's answer with no sign
// that it is doing so.
//
// The period report is the case worth pinning. It is derived from events,
// prices AND income, so a write to any of those has to reach it -- and it is on
// a different query key from the portfolio, so nothing about invalidating the
// portfolio invalidates the report by accident.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useHoldings } from "./useHoldings";

function harness() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidated: unknown[][] = [];
  const original = queryClient.invalidateQueries.bind(queryClient);
  vi.spyOn(queryClient, "invalidateQueries").mockImplementation((filters) => {
    invalidated.push((filters?.queryKey ?? []) as unknown[]);
    return original(filters);
  });

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return { wrapper, invalidated };
}

function invalidatedTheReport(invalidated: unknown[][]) {
  return invalidated.some((key) => key[0] === "portfolio-report");
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useHoldings write invalidation", () => {
  it("refetches the period report after a purchase or sale", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings": { status: 200, body: { holdings: [], notInNetWorth: true } },
      "POST /api/v1/holdings/h1/events": { status: 201, body: { holding: null } },
    });
    const { wrapper, invalidated } = harness();
    const { result } = renderHook(() => useHoldings({ includeArchived: false }), { wrapper });

    await result.current.recordEvent.mutateAsync({
      id: "h1",
      body: { kind: "acquisition", quantity: "10", amountMinor: 100000, occurredOn: "2026-09-12", note: "" },
    });

    // Realised and unrealised both move when a lot is bought or sold, and the
    // report is on its own query key -- invalidating the portfolio does not
    // reach it.
    await waitFor(() => expect(invalidatedTheReport(invalidated)).toBe(true));
  });

  it("refetches the period report after a price is recorded", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings": { status: 200, body: { holdings: [], notInNetWorth: true } },
      "POST /api/v1/holdings/h1/valuations": { status: 200, body: { holding: null } },
    });
    const { wrapper, invalidated } = harness();
    const { result } = renderHook(() => useHoldings({ includeArchived: false }), { wrapper });

    await result.current.recordValuation.mutateAsync({
      id: "h1",
      body: { unitPriceMinor: 12000, asOf: "2026-09-12", note: "" },
    });

    // This is the one the owner actually does: the current period says "no
    // price recorded", they record one, and the answer has to change. A stale
    // report here looks exactly like the feature not working.
    await waitFor(() => expect(invalidatedTheReport(invalidated)).toBe(true));
  });

  it("refetches the period report after a dividend", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings": { status: 200, body: { holdings: [], notInNetWorth: true } },
      "POST /api/v1/holdings/h1/income": { status: 201, body: { income: null } },
    });
    const { wrapper, invalidated } = harness();
    const { result } = renderHook(() => useHoldings({ includeArchived: false }), { wrapper });

    await result.current.recordIncome.mutateAsync({
      id: "h1",
      body: { kind: "income", amountMinor: 4500, receivedOn: "2026-09-12", note: "" },
    });

    await waitFor(() => expect(invalidatedTheReport(invalidated)).toBe(true));
  });
});
