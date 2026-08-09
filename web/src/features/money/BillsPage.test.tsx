// Follows GoalsPage.test.tsx/BudgetPage.test.tsx's own shape: renderWithRouter,
// stubFetchRoutes for every request, literal strings asserted throughout
// (not BILL_COPY's own exports -- BudgetPage.test.tsx's own convention: that
// would make an assertion tautological against a typo in the same module).
//
// The five states from the task brief are each their own `it`, plus the
// three archive/restore tests the brief calls out as "not optional" --
// Goals shipped archiving with no screen that ever called it
// (docs/LEARNING.md pattern 15), and this file is what proves Bills did not
// repeat it.
//
// System time is faked to 2026-08-09 for every test (not only the one that
// needs it) -- BudgetPage.test.tsx's own convention -- so "All caught up"
// always names August and "24 Jul"/"20 Jul" are always in the past without
// this file depending on the real calendar the test runner happens to have.
import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BillsPage } from "./BillsPage";
import type { Bill, BillPayment, BillsResponse, BillsSummary } from "./billSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function billFixture(overrides: Partial<Bill> = {}): Bill {
  return {
    id: "bill-1",
    name: "Netflix",
    amountMinor: 1998,
    currency: "SGD",
    cadence: "monthly",
    nextDue: "2026-08-15",
    categoryId: "cat-1",
    categoryName: "Streaming",
    payFromAccountId: "acct-1",
    accountName: "DBS",
    paidByMembershipId: "membership-1",
    autopay: true,
    isSubscription: true,
    overdue: false,
    dueSoon: true,
    settled: false,
    archivedAt: null,
    ...overrides,
  };
}

function paymentFixture(overrides: Partial<BillPayment> = {}): BillPayment {
  return {
    id: "payment-1",
    billId: "bill-1",
    billName: "Netflix",
    dueOn: "2026-08-05",
    paidOn: "2026-08-05",
    amountMinor: 1998,
    currency: "SGD",
    autopay: true,
    ...overrides,
  };
}

function summaryFixture(overrides: Partial<BillsSummary> = {}): BillsSummary {
  return {
    currency: "SGD",
    dueThisMonthMinor: 0,
    paidSoFarMinor: 0,
    nextDue: null,
    autopayCount: 0,
    billCount: 0,
    subscriptionsMonthlyMinor: 0,
    subscriptionsAnnualMinor: 0,
    excludedNoRate: 0,
    ...overrides,
  };
}

function billsFixture(
  bills: Bill[],
  paidThisMonth: BillPayment[] = [],
  summaryOverrides: Partial<BillsSummary> = {},
): BillsResponse {
  return { bills, paidThisMonth, summary: summaryFixture(summaryOverrides) };
}

function renderPage(response: BillsResponse, extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/bills": { status: 200, body: response },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<BillsPage />) };
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-09T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("BillsPage", () => {
  it("first run: offers Add bill and shows no empty list headings", async () => {
    renderPage(billsFixture([]));

    expect(await screen.findByTestId("bills-empty-state")).toBeInTheDocument();
    // "+ Add bill" is the design's own primary action for this state -- the
    // header's persistent button, not a second empty-state-only button.
    expect(screen.getByTestId("bills-add")).toHaveTextContent("+ Add bill");

    expect(screen.queryByTestId("bills-stat-due-this-month")).not.toBeInTheDocument();
    expect(screen.queryByTestId("bills-stat-paid-so-far")).not.toBeInTheDocument();
    expect(screen.queryByTestId("bills-stat-next-due")).not.toBeInTheDocument();
    expect(screen.queryByText("Due soon")).not.toBeInTheDocument();
    expect(screen.queryByText("Later")).not.toBeInTheDocument();
    expect(screen.queryByText("Paid this month")).not.toBeInTheDocument();
    // billCount === 0 hides the "N of M on autopay" line rather than
    // rendering "0 of 0" -- the formulas table's own rule.
    expect(screen.queryByTestId("bills-subtitle")).not.toBeInTheDocument();
  });

  it("bills exist but none is due this month: the stat cards explain rather than showing bare zeros", async () => {
    const farOut = billFixture({
      id: "b1",
      name: "Home insurance",
      nextDue: "2026-09-20",
      dueSoon: false,
      overdue: false,
      autopay: true,
    });
    renderPage(
      billsFixture([farOut], [], {
        dueThisMonthMinor: 0,
        paidSoFarMinor: 0,
        nextDue: {
          billId: "b1",
          billName: "Home insurance",
          dueOn: "2026-09-20",
          amountMinor: 12000,
          currency: "SGD",
          overdue: false,
          autopay: true,
        },
        autopayCount: 1,
        billCount: 1,
      }),
    );

    // "the stat cards explain rather than showing bare zeros" -- the test's
    // own name -- so this checks for the explanatory text, not merely that
    // the figure happens to be "S$0.00".
    expect(await screen.findByTestId("bills-stat-due-this-month")).toHaveTextContent(
      "S$0.00· Nothing due this month",
    );
    expect(screen.getByTestId("bills-stat-paid-so-far")).toHaveTextContent("S$0.00· Nothing paid yet");

    const dueSoonSection = screen.getByTestId("bills-due-soon");
    expect(within(dueSoonSection).getByText("Nothing due in the next 30 days.")).toBeInTheDocument();

    const laterSection = screen.getByTestId("bills-later");
    expect(within(laterSection).getByText("Home insurance")).toBeInTheDocument();
  });

  it("ordinary: splits Due soon from Later on the server's dueSoon flag", async () => {
    const soon = billFixture({ id: "b1", name: "Car insurance", nextDue: "2026-08-15", dueSoon: true, autopay: true });
    const later = billFixture({
      id: "b2",
      name: "Term insurance",
      nextDue: "2026-11-01",
      dueSoon: false,
      autopay: false,
    });
    const payment = paymentFixture({ id: "p1", billName: "Singtel fibre" });
    renderPage(
      billsFixture([soon, later], [payment], {
        dueThisMonthMinor: 11648,
        paidSoFarMinor: 1998,
        nextDue: {
          billId: "b1",
          billName: "Car insurance",
          dueOn: "2026-08-15",
          amountMinor: 9650,
          currency: "SGD",
          overdue: false,
          autopay: true,
        },
        autopayCount: 1,
        billCount: 2,
      }),
    );

    const dueSoonSection = await screen.findByTestId("bills-due-soon");
    const laterSection = screen.getByTestId("bills-later");
    const paidSection = screen.getByTestId("bills-paid-this-month");

    expect(within(dueSoonSection).getByText("Car insurance")).toBeInTheDocument();
    expect(within(dueSoonSection).queryByText("Term insurance")).not.toBeInTheDocument();
    expect(within(laterSection).getByText("Term insurance")).toBeInTheDocument();
    expect(within(laterSection).queryByText("Car insurance")).not.toBeInTheDocument();
    expect(within(paidSection).getByText("Singtel fibre")).toBeInTheDocument();
  });

  it("all caught up: names the next bill and the month that is settled", async () => {
    const schoolFees = billFixture({
      id: "b2",
      name: "School fees",
      nextDue: "2026-09-15",
      dueSoon: false,
      overdue: false,
      autopay: false,
      categoryName: "Education",
    });
    const payment = paymentFixture({
      id: "p1",
      billId: "b1",
      billName: "SP utilities",
      dueOn: "2026-08-05",
      paidOn: "2026-08-06",
      amountMinor: 38000,
    });
    renderPage(
      billsFixture([schoolFees], [payment], {
        dueThisMonthMinor: 38000,
        paidSoFarMinor: 38000,
        nextDue: {
          billId: "b2",
          billName: "School fees",
          dueOn: "2026-09-15",
          amountMinor: 38000,
          currency: "SGD",
          overdue: false,
          autopay: false,
        },
        autopayCount: 0,
        billCount: 2,
      }),
    );

    const panel = await screen.findByTestId("bills-all-caught-up");
    expect(panel).toHaveTextContent("All caught up");
    expect(panel).toHaveTextContent("everything due in August is paid.");
    expect(panel).toHaveTextContent("Next bill: School fees, 15 Sep.");
  });

  // dueThisMonthMinor only sums a bill whose next_due falls in the CURRENT
  // month -- an overdue bill from a previous month contributes to neither
  // half of the union, so the two totals can agree while that bill still
  // sits, unpaid, in Due soon. Without the `nextDue.overdue !== true` guard
  // this would have rendered "All caught up" directly above "Overdue since
  // 20 Jul" -- caught in review before it shipped, not found by a browser
  // walk.
  it("all caught up stays hidden while an overdue bill from a previous month is still unpaid", async () => {
    const overdue = billFixture({
      id: "b1",
      name: "Property tax",
      nextDue: "2026-07-20",
      overdue: true,
      dueSoon: true,
      autopay: false,
    });
    const payment = paymentFixture({ id: "p1", billName: "Netflix", dueOn: "2026-08-05" });
    renderPage(
      billsFixture([overdue], [payment], {
        // Equal, both positive, and nothing excluded -- every other clause
        // of the allCaughtUp condition holds; only the overdue guard stops
        // the panel here.
        dueThisMonthMinor: 1998,
        paidSoFarMinor: 1998,
        nextDue: {
          billId: "b1",
          billName: "Property tax",
          dueOn: "2026-07-20",
          amountMinor: 23000,
          currency: "SGD",
          overdue: true,
          autopay: false,
        },
        autopayCount: 0,
        billCount: 1,
      }),
    );

    await screen.findByText("Property tax");
    expect(screen.queryByTestId("bills-all-caught-up")).not.toBeInTheDocument();
  });

  it("overdue: sorts first, and an autopay bill's copy differs from a manual one's", async () => {
    const overdueManual = billFixture({
      id: "b1",
      name: "Property tax",
      nextDue: "2026-07-20",
      overdue: true,
      dueSoon: true,
      autopay: false,
      categoryName: "Tax",
    });
    const overdueAutopay = billFixture({
      id: "b2",
      name: "Car insurance",
      nextDue: "2026-07-24",
      overdue: true,
      dueSoon: true,
      autopay: true,
      categoryName: "Insurance",
    });
    const normalDueSoon = billFixture({
      id: "b3",
      name: "Netflix",
      nextDue: "2026-08-15",
      overdue: false,
      dueSoon: true,
      autopay: true,
    });
    renderPage(
      // Fed in the server's own next_due-ascending order (BillsPage.tsx
      // never re-sorts) -- both overdue bills' earlier dates are what put
      // them ahead of the ordinary due-soon row.
      billsFixture([overdueManual, overdueAutopay, normalDueSoon], [], {
        dueThisMonthMinor: 1000,
        paidSoFarMinor: 0,
        nextDue: {
          billId: "b1",
          billName: "Property tax",
          dueOn: "2026-07-20",
          amountMinor: 23000,
          currency: "SGD",
          overdue: true,
          autopay: false,
        },
        autopayCount: 2,
        billCount: 3,
      }),
    );

    const dueSoonSection = await screen.findByTestId("bills-due-soon");
    const rows = within(dueSoonSection).getAllByTestId("bill-row");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Property tax");
    expect(rows[1]).toHaveTextContent("Car insurance");
    expect(rows[2]).toHaveTextContent("Netflix");

    expect(within(dueSoonSection).getByText("Overdue since 20 Jul")).toBeInTheDocument();
    expect(
      within(dueSoonSection).getByText("Should have gone out on 24 Jul — confirm it did"),
    ).toBeInTheDocument();

    const nextDueCard = screen.getByTestId("bills-stat-next-due");
    expect(nextDueCard).toHaveTextContent("Overdue");
    expect(nextDueCard).toHaveTextContent("Property tax");
    // Names it as overdue rather than printing the past date as though it
    // were upcoming (state 5's own contract).
    expect(nextDueCard).not.toHaveTextContent("Jul 20");
  });

  it("a settled one-off appears under Later with 'Settled' where a date would go", async () => {
    const settled = billFixture({
      id: "b1",
      name: "Piano tuning",
      cadence: "one_off",
      nextDue: null,
      settled: true,
      dueSoon: false,
      overdue: false,
      autopay: false,
    });
    renderPage(
      billsFixture([settled], [], {
        dueThisMonthMinor: 0,
        paidSoFarMinor: 0,
        nextDue: null,
        autopayCount: 0,
        billCount: 1,
      }),
    );

    const laterSection = await screen.findByTestId("bills-later");
    expect(within(laterSection).getByText("Piano tuning")).toBeInTheDocument();
    expect(within(laterSection).getByText("Settled")).toBeInTheDocument();

    // Neither the Due soon list nor its own row set contains it -- a settled
    // one-off satisfies neither list's own 30-day/overdue definition.
    const dueSoonSection = screen.getByTestId("bills-due-soon");
    expect(within(dueSoonSection).queryByText("Piano tuning")).not.toBeInTheDocument();
  });

  it("every live bill row carries an Archive control", async () => {
    const soon = billFixture({ id: "b1", name: "Car insurance", dueSoon: true, nextDue: "2026-08-15" });
    const later = billFixture({ id: "b2", name: "Term insurance", dueSoon: false, nextDue: "2026-11-01" });
    renderPage(
      billsFixture([soon, later], [], {
        dueThisMonthMinor: 0,
        paidSoFarMinor: 0,
        nextDue: null,
        autopayCount: 2,
        billCount: 2,
      }),
    );

    await screen.findByTestId("bills-due-soon");
    expect(screen.getByRole("button", { name: "Archive Car insurance" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Archive Term insurance" })).toBeInTheDocument();
  });

  // The presence test above proves the control renders; this proves it
  // actually does something when clicked, the same depth GoalsPage.test.tsx's
  // own archive/restore tests go to rather than stopping at "is on screen."
  // Only `billsQueryKey(false)` has an active observer here (the toggle is
  // never switched on in this test), so useArchiveBill's own both-variants
  // invalidation only has to be stubbed for the one variant actually mounted.
  it("clicking Archive calls the archive route and the bill moves out of the live lists", async () => {
    const live = billFixture({ id: "b1", name: "Car insurance", dueSoon: true, nextDue: "2026-08-15" });
    renderPage(billsFixture([live], [], { autopayCount: 1, billCount: 1 }), {
      "GET /api/v1/bills": [
        { status: 200, body: billsFixture([live], [], { autopayCount: 1, billCount: 1 }) },
        { status: 200, body: billsFixture([], [], { autopayCount: 0, billCount: 0 }) },
      ],
      "POST /api/v1/bills/b1/archive": {
        status: 200,
        body: { bill: { ...live, archivedAt: "2026-08-09T00:00:00Z" } },
      },
    });

    await screen.findByText("Car insurance");
    fireEvent.click(screen.getByRole("button", { name: "Archive Car insurance" }));

    expect(await screen.findByTestId("bills-empty-state")).toBeInTheDocument();
  });

  it("Show archived lists archived bills, each with Restore", async () => {
    const live = billFixture({ id: "b1", name: "Car insurance", dueSoon: true, nextDue: "2026-08-15" });
    const archived = billFixture({
      id: "b2",
      name: "Old gym membership",
      archivedAt: "2026-06-01T00:00:00Z",
      dueSoon: false,
      nextDue: "2026-05-01",
    });

    renderPage(
      billsFixture([live], [], { dueThisMonthMinor: 0, paidSoFarMinor: 0, nextDue: null, autopayCount: 1, billCount: 1 }),
      {
        "GET /api/v1/bills?include_archived=true": {
          status: 200,
          body: billsFixture([live, archived], [], {
            dueThisMonthMinor: 0,
            paidSoFarMinor: 0,
            nextDue: null,
            autopayCount: 1,
            billCount: 1,
          }),
        },
      },
    );

    expect(await screen.findByText("Car insurance")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));

    const archivedSection = await screen.findByTestId("bills-archived-section");
    expect(within(archivedSection).getByText("Old gym membership")).toBeInTheDocument();
    expect(
      within(archivedSection).getByRole("button", { name: "Restore Old gym membership" }),
    ).toBeInTheDocument();
    expect(
      within(archivedSection).queryByRole("button", { name: "Archive Old gym membership" }),
    ).not.toBeInTheDocument();
  });

  it("an archived bill is in neither Due soon nor Later, and counts in no stat card", async () => {
    const live = billFixture({ id: "b1", name: "Car insurance", dueSoon: true, nextDue: "2026-08-15", autopay: true });
    const archived = billFixture({
      id: "b2",
      name: "Old gym membership",
      archivedAt: "2026-06-01T00:00:00Z",
      dueSoon: false,
      nextDue: "2026-05-01",
      autopay: true,
    });
    const summary: Partial<BillsSummary> = {
      dueThisMonthMinor: 9650,
      paidSoFarMinor: 0,
      nextDue: {
        billId: "b1",
        billName: "Car insurance",
        dueOn: "2026-08-15",
        amountMinor: 9650,
        currency: "SGD",
        overdue: false,
        autopay: true,
      },
      autopayCount: 1,
      billCount: 1,
    };

    renderPage(billsFixture([live], [], summary), {
      "GET /api/v1/bills?include_archived=true": {
        status: 200,
        body: billsFixture([live, archived], [], summary),
      },
    });

    await screen.findByText("Car insurance");
    // The header count is unaffected by the archived bill even once it is
    // in view -- summary counts live bills only, whether or not the toggle
    // is on (billsSummarySchema's own comment).
    expect(screen.getByTestId("bills-subtitle")).toHaveTextContent("1 of 1 on autopay");

    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));
    await screen.findByTestId("bills-archived-section");

    const dueSoonSection = screen.getByTestId("bills-due-soon");
    expect(within(dueSoonSection).queryByText("Old gym membership")).not.toBeInTheDocument();
    // laterBills is empty here (the only non-due-soon bill is the archived
    // one, which is filtered out before the Due soon/Later split runs) --
    // so the whole Later section is absent, not merely empty of this name.
    expect(screen.queryByTestId("bills-later")).not.toBeInTheDocument();
    expect(screen.getByTestId("bills-subtitle")).toHaveTextContent("1 of 1 on autopay");
  });
});
