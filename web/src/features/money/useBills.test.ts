import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import {
  useArchiveBill,
  useBills,
  useCreateBill,
  useMarkPaid,
  useRestoreBill,
  useUndoPayment,
  useUpdateBill,
} from "./useBills";

const bill = {
  id: "bill-1",
  name: "Netflix",
  amountMinor: 1999,
  currency: "SGD",
  cadence: "monthly",
  nextDue: "2026-09-01",
  categoryId: "cat-1",
  categoryName: "Subscriptions",
  payFromAccountId: "acct-1",
  accountName: "Joint checking",
  paidByMembershipId: "membership-1",
  autopay: true,
  isSubscription: true,
  overdue: false,
  dueSoon: true,
  settled: false,
  archivedAt: null,
};

const billsResponse = {
  bills: [bill],
  paidThisMonth: [],
  summary: {
    currency: "SGD",
    dueThisMonthMinor: 1999,
    paidSoFarMinor: 0,
    nextDue: {
      billId: "bill-1",
      billName: "Netflix",
      dueOn: "2026-09-01",
      amountMinor: 1999,
      currency: "SGD",
      overdue: false,
      autopay: true,
    },
    autopayCount: 1,
    billCount: 1,
    subscriptionsMonthlyMinor: 1999,
    subscriptionsAnnualMinor: 0,
    excludedNoRate: 0,
  },
};

const payment = {
  id: "payment-1",
  billId: "bill-1",
  billName: "Netflix",
  dueOn: "2026-09-01",
  paidOn: "2026-09-01",
  amountMinor: 1999,
  currency: "SGD",
  autopay: true,
};

function renderUseBills(includeArchived = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(() => useBills(includeArchived), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

// Renders useBills(false) alongside one mutation hook, sharing a QueryClient
// -- the same wiring a real page gets (both hooks read the same cache), and
// the only way to prove a mutation's onSuccess actually invalidates and
// refetches the list rather than the hook still showing whatever the first
// GET returned (the useGoals.test.ts precedent this file otherwise follows,
// adapted for useBills.ts's useAccounts.ts-shaped separate hooks instead of
// one hook exposing every write as a method).
function renderBillsWith<T>(useExtra: () => T, includeArchived = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(
    () => ({ bills: useBills(includeArchived), extra: useExtra() }),
    { wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children) },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useBills", () => {
  it("GETs /api/v1/bills on mount", async () => {
    stubFetchRoutes({
      "GET /api/v1/bills": { status: 200, body: billsResponse },
    });

    const { result } = renderUseBills();

    expect(result.current.isLoading).toBe(true);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.bills).toHaveLength(1);
    expect(result.current.data?.bills[0].name).toBe("Netflix");
  });

  // include_archived is spelled the way account_handlers.go and
  // goal_handlers.go already spell it (bill_handlers.go's own
  // handleListBills comment), so this pins that this hook spells it
  // identically rather than inventing a second convention.
  it("GETs with ?include_archived=true when includeArchived is set", async () => {
    stubFetchRoutes({
      "GET /api/v1/bills?include_archived=true": { status: 200, body: billsResponse },
    });

    const { result } = renderUseBills(true);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.data?.bills).toHaveLength(1);
  });

  // billDTO's own comment: a settled one-off (paid, no next occurrence)
  // satisfies neither Due soon's nor Later's own 30-day/overdue definition,
  // yet the header's autopay/bill counts still include it -- the exact gap
  // Task 7's review found in an earlier sketch of this DTO. If `settled`
  // were ever dropped from billSchema, this bill would still round-trip
  // (zod only rejects a *missing required* field, and every other field
  // here is unaffected), but `.settled` would read `undefined` instead of
  // `true` -- which is what this test actually pins.
  it("keeps `settled` on a paid one-off with no next occurrence", async () => {
    const settledBill = {
      ...bill,
      id: "bill-2",
      name: "Piano tuning",
      cadence: "one_off",
      nextDue: null,
      settled: true,
    };
    stubFetchRoutes({
      "GET /api/v1/bills": {
        status: 200,
        body: { ...billsResponse, bills: [settledBill] },
      },
    });

    const { result } = renderUseBills();
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.data?.bills[0].settled).toBe(true);
    expect(result.current.data?.bills[0].nextDue).toBeNull();
  });

  it("surfaces a fetch failure as `error`, not a thrown exception", async () => {
    stubFetchRoutes({
      "GET /api/v1/bills": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    const { result } = renderUseBills();
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.error).not.toBeNull();
    expect(result.current.data).toBeUndefined();
  });

  // GET /bills is money AND owner-gated (task-9-report.md's own note, the
  // same shape GET /budgets/{month} carries), so Overview (Task 16) -- which
  // renders for every member, owner or not -- has to be able to mount this
  // hook without firing a doomed 403. Mirrors useGoalContributions' own
  // "only while enabled" test: no request at all while disabled, then a
  // real fetch once a rerender flips it on.
  it("does not fire GET /api/v1/bills while enabled is false", async () => {
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/bills": { status: 200, body: billsResponse },
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) => useBills(false, { enabled }),
      {
        initialProps: { enabled: false },
        wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
      },
    );

    expect(result.current.isLoading).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();

    rerender({ enabled: true });
    await waitFor(() => expect(result.current.data?.bills).toHaveLength(1));
  });

  it("useCreateBill POSTs the exact body and returns the parsed bill", async () => {
    let postBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/bills": [
        { status: 200, body: { ...billsResponse, bills: [] } },
        { status: 200, body: billsResponse },
      ],
      "POST /api/v1/bills": {
        status: 201,
        body: { bill },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderBillsWith(() => useCreateBill());
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));

    const body = {
      name: "Netflix",
      amountMinor: 1999,
      cadence: "monthly" as const,
      nextDue: "2026-09-01",
      categoryId: "cat-1",
      payFromAccountId: "acct-1",
      paidByMembershipId: "membership-1",
      autopay: true,
      isSubscription: true,
    };
    let created: unknown;
    await act(async () => {
      created = await result.current.extra.mutateAsync(body);
    });

    expect(postBody).toEqual(body);
    expect(created).toEqual(bill);
    // Also proves the write invalidates the bills list, the same
    // fire-a-mutation-then-see-the-list-move shape every write below pins.
    await waitFor(() => expect(result.current.bills.data?.bills).toHaveLength(1));
  });

  it("useUpdateBill PATCHes /api/v1/bills/{id} and returns the parsed bill", async () => {
    let patchBody: unknown;
    const renamedBill = { ...bill, name: "Netflix (shared)" };
    stubFetchRoutes({
      "GET /api/v1/bills": [
        { status: 200, body: billsResponse },
        { status: 200, body: { ...billsResponse, bills: [renamedBill] } },
      ],
      "PATCH /api/v1/bills/bill-1": {
        status: 200,
        body: { bill: renamedBill },
        capture: (body) => {
          patchBody = body;
        },
      },
    });

    const { result } = renderBillsWith(() => useUpdateBill());
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));

    let updated: unknown;
    await act(async () => {
      updated = await result.current.extra.mutateAsync({
        id: "bill-1",
        body: { name: "Netflix (shared)" },
      });
    });

    expect(patchBody).toEqual({ name: "Netflix (shared)" });
    expect(updated).toEqual(renamedBill);
    await waitFor(() => expect(result.current.bills.data?.bills[0].name).toBe("Netflix (shared)"));
  });

  it("useArchiveBill POSTs /api/v1/bills/{id}/archive then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/bills": [
        { status: 200, body: billsResponse },
        { status: 200, body: { ...billsResponse, bills: [] } },
      ],
      "POST /api/v1/bills/bill-1/archive": {
        status: 200,
        body: { bill: { ...bill, archivedAt: "2026-09-01T00:00:00Z" } },
      },
    });

    const { result } = renderBillsWith(() => useArchiveBill());
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));

    await act(async () => {
      await result.current.extra.mutateAsync("bill-1");
    });

    await waitFor(() => expect(result.current.bills.data?.bills).toHaveLength(0));
  });

  // Every mutation invalidates BOTH billsQueryKey variants (invalidateBills'
  // own comment), not just the `includeArchived: false` one every other
  // test in this file renders -- a write performed while "Show archived" is
  // on must not leave that variant's own cache stale. Rendering useBills(true)
  // here, not useBills(false) like every test above, is what a dropped
  // `billsQueryKey(true)` invalidation would slip past: every test above
  // would still stay green (proven -- see this task's own mutation check),
  // since none of them ever asks for the archived-included variant at all.
  it("useArchiveBill also refreshes the include_archived=true variant", async () => {
    const archivedBill = { ...bill, archivedAt: "2026-09-01T00:00:00Z" };
    stubFetchRoutes({
      "GET /api/v1/bills?include_archived=true": [
        { status: 200, body: billsResponse },
        { status: 200, body: { ...billsResponse, bills: [archivedBill] } },
      ],
      "POST /api/v1/bills/bill-1/archive": {
        status: 200,
        body: { bill: archivedBill },
      },
    });

    const { result } = renderBillsWith(() => useArchiveBill(), true);
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));
    expect(result.current.bills.data?.bills[0].archivedAt).toBeNull();

    await act(async () => {
      await result.current.extra.mutateAsync("bill-1");
    });

    await waitFor(() =>
      expect(result.current.bills.data?.bills[0].archivedAt).toBe("2026-09-01T00:00:00Z"),
    );
  });

  it("useRestoreBill POSTs /api/v1/bills/{id}/restore then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/bills": [
        { status: 200, body: { ...billsResponse, bills: [] } },
        { status: 200, body: billsResponse },
      ],
      "POST /api/v1/bills/bill-1/restore": {
        status: 200,
        body: { bill },
      },
    });

    const { result } = renderBillsWith(() => useRestoreBill());
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));

    await act(async () => {
      await result.current.extra.mutateAsync("bill-1");
    });

    await waitFor(() => expect(result.current.bills.data?.bills).toHaveLength(1));
  });

  // billPaymentResponse's own comment: the bill half already carries the
  // full joined view (nextDue advanced) so a caller never needs a second
  // GET to see what paying just changed -- pinned here by asserting the
  // mutation's own return value, not just the refetch.
  it("useMarkPaid POSTs /api/v1/bills/{id}/pay and returns both the payment and the bill", async () => {
    let postBody: unknown;
    const paidBill = { ...bill, nextDue: "2026-10-01" };
    stubFetchRoutes({
      "GET /api/v1/bills": [
        { status: 200, body: billsResponse },
        { status: 200, body: { ...billsResponse, bills: [paidBill], paidThisMonth: [payment] } },
      ],
      "POST /api/v1/bills/bill-1/pay": {
        status: 200,
        body: { payment, bill: paidBill },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderBillsWith(() => useMarkPaid());
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));

    let paid: unknown;
    await act(async () => {
      paid = await result.current.extra.mutateAsync({
        id: "bill-1",
        body: { paidOn: "2026-09-01" },
      });
    });

    expect(postBody).toEqual({ paidOn: "2026-09-01" });
    expect(paid).toEqual({ payment, bill: paidBill });
    await waitFor(() => expect(result.current.bills.data?.paidThisMonth).toHaveLength(1));
  });

  // DELETE answers 204 with no body -- the one status apiFetch does not try
  // to parse. A schema-parse or a `.json()` read here would throw against an
  // empty body, so this pins that useUndoPayment tolerates it, the same
  // shape useGoals.test.ts's deleteContribution test pins for its own 204.
  it("useUndoPayment DELETEs and tolerates the bodyless 204", async () => {
    stubFetchRoutes({
      "GET /api/v1/bills": [
        { status: 200, body: { ...billsResponse, paidThisMonth: [payment] } },
        { status: 200, body: billsResponse },
      ],
      "DELETE /api/v1/bills/bill-1/payments/payment-1": { status: 204, body: undefined },
    });

    const { result } = renderBillsWith(() => useUndoPayment());
    await waitFor(() => expect(result.current.bills.isLoading).toBe(false));
    expect(result.current.bills.data?.paidThisMonth).toHaveLength(1);

    await act(async () => {
      await result.current.extra.mutateAsync({ billId: "bill-1", paymentId: "payment-1" });
    });

    await waitFor(() => expect(result.current.bills.data?.paidThisMonth).toHaveLength(0));
  });
});
