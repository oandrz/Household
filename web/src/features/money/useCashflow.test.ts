import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useUpdateHousehold } from "../settings/useHousehold";
import { cashflowPageSchema } from "./cashflowSchemas";
import { useCashflow, useConfirmCashflowMonth, useUnconfirmCashflowMonth } from "./useCashflow";
import { useUpdateAccount } from "./useAccounts";
import { useMarkPaid, useUpdateBill } from "./useBills";
import { useCreateTransaction } from "./useTransactions";

const month = {
  month: "2026-09",
  status: "solid",
  inMinor: 500000,
  outMinor: 400000,
  leftMinor: 100000,
  keptBasisPoints: null,
  spreadMinor: 17500,
  loanMinor: 0,
  transactionCount: 6,
  grey: [],
  unconvertibleCurrencies: [],
  projectedLeftMinor: null,
  confirmation: null,
  contributions: [{ kind: "spread_bill", name: "Car insurance", amountMinor: 17500, paidOn: "2026-03-14" }],
};

const page = {
  currency: "SGD",
  hasTransactions: true,
  oldestYear: 2024,
  light: {
    colour: "yellow",
    reasons: [{ code: "kept_under_10", keptBasisPoints: 600, cushionTenths: null, billName: "", billUnconvertible: false }],
  },
  average: { months: 3, inMinor: 0, outMinor: 0, leftMinor: 0, keptBasisPoints: 1200 },
  cushion: { known: true, tenths: 42, reason: "" },
  lastFinished: month,
  current: { ...month, month: "2026-10", status: "current" },
  recent: [month],
  year: {
    year: 2026,
    months: [month],
    keptSoFarMinor: 0,
    incomeMinor: 0,
    greyMonths: 1,
    keptBasisPoints: 1600,
    sameMonthsLastYearMinor: null,
    headingForMinor: null,
    billsToCome: [
      { billId: "bill-1", name: "Car insurance", amountMinor: 210000, nextDue: "2026-12-01", unconvertible: false },
    ],
  },
};

const confirmed = { month: "2026-08", confirmedAt: "2026-10-25T08:00:00Z", confirmedByName: "Ana" };

afterEach(() => {
  vi.unstubAllGlobals();
});

// Renders the cashflow query beside one write hook on a shared QueryClient,
// the wiring a real page gets, so a write's invalidation is observable as a
// second GET.
function renderWith<T>(useExtra: () => T) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => ({ cashflow: useCashflow(2026), extra: useExtra() }), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

function cashflowGets(fetchMock: ReturnType<typeof stubFetchRoutes>) {
  return fetchMock.mock.calls.filter(([url, init]) => {
    return String(url) === "/api/v1/cashflow?year=2026" && (init?.method ?? "GET") === "GET";
  }).length;
}

const cashflowRoute = { "GET /api/v1/cashflow?year=2026": { status: 200, body: page } };

describe("cashflowPageSchema", () => {
  it("parses a full cashflow response", () => {
    const parsed = cashflowPageSchema.parse(page);
    expect(parsed.light.colour).toBe("yellow");
    expect(parsed.year.billsToCome[0].nextDue).toBe("2026-12-01");
    expect(parsed.recent[0].contributions[0].kind).toBe("spread_bill");
  });

  it("rejects an unknown light colour", () => {
    expect(() => cashflowPageSchema.parse({ ...page, light: { ...page.light, colour: "blue" } })).toThrow();
  });
});

describe("useCashflow", () => {
  it("omits the year parameter when no year is chosen", async () => {
    const fetchMock = stubFetchRoutes({ "GET /api/v1/cashflow": { status: 200, body: page } });
    const { result } = renderHook(() => useCashflow(undefined), {
      wrapper: ({ children }) =>
        createElement(QueryClientProvider, { client: new QueryClient() }, children),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("confirming a month refetches the cashflow page", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "PUT /api/v1/cashflow/confirmations/2026-08": { status: 200, body: confirmed },
    });
    const { result } = renderWith(() => useConfirmCashflowMonth());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    let response;
    await act(async () => {
      response = await result.current.extra.mutateAsync("2026-08");
    });

    expect(response).toEqual(confirmed);
    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });

  it("undoing a confirmation refetches the cashflow page", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "DELETE /api/v1/cashflow/confirmations/2026-08": { status: 204, body: undefined },
    });
    const { result } = renderWith(() => useUnconfirmCashflowMonth());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    await act(async () => {
      await result.current.extra.mutateAsync("2026-08");
    });

    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });
});

describe("writes that change Cashflow's inputs", () => {
  const transaction = {
    id: "txn-1",
    kind: "expense",
    occurredOn: "2026-10-01",
    description: "Lunch",
    categoryId: null,
    categoryName: null,
    paidByMembershipId: null,
    paidByName: null,
    fromAccountId: "acct-1",
    fromAccountName: "Joint",
    toAccountId: null,
    toAccountName: null,
    amount: { amountMinor: 100, currency: "SGD" },
    receivedAmount: null,
    beforeFromAccountOpeningBalance: null,
    beforeToAccountOpeningBalance: null,
  };

  it("creating a transaction refetches cashflow", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "POST /api/v1/transactions": { status: 200, body: transaction },
    });
    const { result } = renderWith(() => useCreateTransaction());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    await act(async () => {
      await result.current.extra.mutateAsync({});
    });

    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });

  it("paying a bill refetches cashflow", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "POST /api/v1/bills/bill-1/pay": { status: 200, body: payResponse },
    });
    const { result } = renderWith(() => useMarkPaid());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    await act(async () => {
      await result.current.extra.mutateAsync({ id: "bill-1", body: { paidOn: "2026-10-01" } });
    });

    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });

  it("editing a bill refetches cashflow", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "PATCH /api/v1/bills/bill-1": { status: 200, body: { bill } },
    });
    const { result } = renderWith(() => useUpdateBill());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    await act(async () => {
      await result.current.extra.mutateAsync({ id: "bill-1", body: { cadence: "yearly" } });
    });

    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });

  it("editing an account refetches cashflow", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "PATCH /api/v1/accounts/acct-1": { status: 200, body: account },
    });
    const { result } = renderWith(() => useUpdateAccount());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    await act(async () => {
      await result.current.extra.mutateAsync({
        id: "acct-1",
        nickname: "Joint",
        type: "cash",
        ownerMembershipId: null,
        countTowardNetWorth: true,
        visibleToLimitedMembers: true,
        openingBalanceAsOf: "2026-01-01",
      });
    });

    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });

  it("changing the primary currency refetches cashflow", async () => {
    const fetchMock = stubFetchRoutes({
      ...cashflowRoute,
      "PATCH /api/v1/household": { status: 200, body: household },
    });
    const { result } = renderWith(() => useUpdateHousehold());
    await waitFor(() => expect(result.current.cashflow.isSuccess).toBe(true));

    await act(async () => {
      await result.current.extra.mutateAsync({ primaryCurrency: "USD" });
    });

    await waitFor(() => expect(cashflowGets(fetchMock)).toBe(2));
  });
});

const bill = {
  id: "bill-1",
  name: "Car insurance",
  amountMinor: 210000,
  currency: "SGD",
  cadence: "yearly",
  nextDue: "2026-12-01",
  categoryId: "cat-1",
  categoryName: "Insurance",
  payFromAccountId: "acct-1",
  accountName: "Joint",
  paidByMembershipId: "membership-1",
  autopay: false,
  isSubscription: false,
  overdue: false,
  dueSoon: false,
  settled: false,
  archivedAt: null,
};

const payResponse = {
  payment: {
    id: "pay-1",
    billId: "bill-1",
    billName: "Car insurance",
    dueOn: "2026-12-01",
    paidOn: "2026-10-01",
    amountMinor: 210000,
    currency: "SGD",
    autopay: false,
  },
  bill,
};

const account = {
  id: "acct-1",
  nickname: "Joint",
  type: "cash",
  ownerMembershipId: null,
  ownerName: null,
  countTowardNetWorth: true,
  visibleToLimitedMembers: true,
  archivedAt: null,
};

const household = {
  id: "h-1",
  name: "Home",
  familyName: "Tan",
  primaryCurrency: "USD",
  showSecondaryCurrency: false,
  secondaryCurrency: "",
  fxRateMode: "manual",
  timezone: "UTC",
};
