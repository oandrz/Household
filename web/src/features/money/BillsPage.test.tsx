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

// BillsPage itself calls useAccounts(false) -- a bill is paid FROM an
// account, so with none there is nothing to add a bill against and both
// Add-bill entry points are disabled (BillsPage.tsx's own comment). Every
// test here registers the route; the one that is about the no-accounts state
// overrides it with an empty list.
const ACCOUNTS = {
  status: 200,
  body: {
    accounts: [
      {
        id: "acct-1",
        nickname: "DBS",
        type: "cash",
        ownerMembershipId: null,
        ownerName: null,
        balance: { amountMinor: 500000, currency: "SGD" },
        countTowardNetWorth: true,
        visibleToLimitedMembers: false,
        archivedAt: null,
      },
    ],
  },
};

const NO_ACCOUNTS = { status: 200, body: { accounts: [] } };

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
    "GET /api/v1/accounts": ACCOUNTS,
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

  // A household with no accounts cannot create a bill at all:
  // pay_from_account_id is NOT NULL and is where a bill's currency comes
  // from, so "+ Add bill" opened a modal whose Pay from select had nothing
  // in it -- browser constraint validation on submit, no explanation, and
  // first-run reachable. Both sibling screens already refuse this class, and
  // one of them (QuickAddMenu) refuses it for this very modal.
  it("no accounts: both Add-bill entry points are refused, with the way out on screen", async () => {
    renderPage(billsFixture([]), { "GET /api/v1/accounts": NO_ACCOUNTS });

    expect(await screen.findByTestId("bills-empty-state")).toBeInTheDocument();
    expect(screen.getByTestId("bills-add")).toBeDisabled();
    expect(screen.getByText("Add an account first, and bills can be paid from it.")).toBeInTheDocument();

    // The empty state explains the same thing and links to Finances, rather
    // than offering a second button into the same dead end.
    expect(screen.queryByTestId("bills-create-first")).not.toBeInTheDocument();
    expect(screen.getByText("Add an account first")).toBeInTheDocument();
    expect(screen.getByTestId("bills-add-account")).toHaveAttribute("href", "/money");
  });

  // The other half of the guard: only a query that has actually answered may
  // say a household has no accounts. `accounts.data?.accounts.length ?? 0`
  // would read every not-yet-answered query -- still loading, or failed --
  // as "zero accounts" and disable Add bill for every household on first
  // paint, a new defect in place of the one being fixed. A failed GET is the
  // reachable, deterministic form of "never answered"; the modal has its own
  // message for what the household meets next (BillModal.tsx's own
  // accounts-error branch).
  it("accounts unavailable: Add bill is not disabled on a query that never answered", async () => {
    renderPage(billsFixture([]), {
      "GET /api/v1/accounts": { status: 500, body: { error: { code: "INTERNAL", message: "nope" } } },
    });

    expect(await screen.findByTestId("bills-empty-state")).toBeInTheDocument();
    expect(screen.getByTestId("bills-add")).toBeEnabled();
    expect(screen.getByTestId("bills-create-first")).toBeInTheDocument();
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

    // Waits on the section itself, not the bill's own name -- SubscriptionsCard
    // (Task 15) renders every isSubscription bill's name too (billFixture's
    // own default is `isSubscription: true`), so a bare findByText("Property
    // tax") is now ambiguous between the row here and that panel's own row
    // for the identical bill.
    await screen.findByTestId("bills-due-soon");
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

    // SubscriptionsCard (Task 15) renders this same bill's name a second
    // time (billFixture's own default is `isSubscription: true`), so this
    // waits on the row's own Archive control -- unambiguous, since that
    // panel has no buttons at all -- rather than the bill's bare name.
    await screen.findByRole("button", { name: "Archive Car insurance" });
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

    // Scoped to Due soon -- SubscriptionsCard (Task 15) renders this same
    // bill's name a second time (billFixture's own default is
    // `isSubscription: true`), so a bare findByText("Car insurance") is
    // ambiguous between the two panels.
    expect(await within(await screen.findByTestId("bills-due-soon")).findByText("Car insurance")).toBeInTheDocument();
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

    // SubscriptionsCard (Task 15) renders this same bill's name a second
    // time (billFixture's own default is `isSubscription: true`), so this
    // waits on the section rather than the bill's bare name.
    await screen.findByTestId("bills-due-soon");
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

  // Goals shipped archiving with every layer beneath it built and no screen
  // that called it -- "Show archived" and Restore led out of a state no
  // household could enter (docs/LEARNING.md pattern 15). Every archive/
  // restore test above still leaves at least one live bill standing, so
  // none of them actually proves the toggle and the archived section are
  // still reachable once a household archives its *only* live bill --
  // exactly the shape that defect had. The empty-state gate (`noLiveBills`)
  // and the archived section are independent sibling blocks in
  // BillsPage.tsx, not nested inside one another; this is what pins that
  // shape against a future edit that nests them.
  it("archiving a household's only live bill still leaves the empty state, the toggle and the archived section all reachable", async () => {
    const archived = billFixture({
      id: "b1",
      name: "Old gym membership",
      archivedAt: "2026-06-01T00:00:00Z",
      dueSoon: false,
      nextDue: "2026-05-01",
    });

    renderPage(
      billsFixture([], [], { dueThisMonthMinor: 0, paidSoFarMinor: 0, nextDue: null, autopayCount: 0, billCount: 0 }),
      {
        "GET /api/v1/bills?include_archived=true": {
          status: 200,
          body: billsFixture([archived], [], {
            dueThisMonthMinor: 0,
            paidSoFarMinor: 0,
            nextDue: null,
            autopayCount: 0,
            billCount: 0,
          }),
        },
      },
    );

    expect(await screen.findByTestId("bills-empty-state")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));

    // Both blocks render together once the toggle is on -- neither hides
    // the other, which is exactly the household's way back from having
    // archived its last live bill.
    expect(await screen.findByTestId("bills-empty-state")).toBeInTheDocument();
    const archivedSection = screen.getByTestId("bills-archived-section");
    expect(within(archivedSection).getByText("Old gym membership")).toBeInTheDocument();
    expect(
      within(archivedSection).getByRole("button", { name: "Restore Old gym membership" }),
    ).toBeInTheDocument();
    // Direction one of the `noneArchived` branch (BillsPage.tsx:171-176,
    // previously untested in either direction): something IS archived, so
    // "No archived bills." must not also render alongside the section that
    // just listed it.
    expect(screen.queryByTestId("bills-archived-empty")).not.toBeInTheDocument();
  });

  // Direction two of the same `noneArchived` branch: nothing archived, with
  // a live bill still on screen so this exercises the populated branch, not
  // the empty-state one above -- AccountsPanel.tsx's own `noneArchived`
  // shape, restated for Bills and, until now, never actually asserted here.
  it("Show archived says so when nothing is archived, even with live bills still on screen", async () => {
    const live = billFixture({ id: "b1", name: "Car insurance", dueSoon: true, nextDue: "2026-08-15" });
    renderPage(billsFixture([live], [], { autopayCount: 1, billCount: 1 }), {
      "GET /api/v1/bills?include_archived=true": {
        status: 200,
        body: billsFixture([live], [], { autopayCount: 1, billCount: 1 }),
      },
    });

    // SubscriptionsCard (Task 15) renders this same bill's name a second
    // time (billFixture's own default is `isSubscription: true`), so this
    // waits on the section rather than the bill's bare name.
    await screen.findByTestId("bills-due-soon");
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));

    expect(await screen.findByTestId("bills-archived-empty")).toHaveTextContent("No archived bills.");
    expect(screen.queryByTestId("bills-archived-section")).not.toBeInTheDocument();
  });

  // The Task 18 browser walk found this: GET /bills is money AND
  // owner-gated exactly like GET /goals, but this page answered every
  // failure -- a genuine 500 and a routine "you're not the owner" 403 --
  // with the identical scary red alert, where GoalsPage.tsx's own copy
  // distinguishes them. A limited member holding money who follows the
  // sidebar's own Bills link (moneyGuardRoute only checks the capability,
  // never the role) landed on "Couldn't load your bills," which reads as
  // broken rather than as the ordinary, expected boundary it is. Mirrors
  // GoalsPage.test.tsx's own "renders the owner-only explanation" test
  // verbatim in shape, the same interim-Overview pattern-2 reasoning: this
  // asserts the explanation's *presence*, not merely the generic error's
  // absence.
  it("a 403 from GET /bills renders the owner-only explanation, not the generic load error", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/bills": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Only an owner may do that." } },
      },
    });

    renderWithRouter(<BillsPage />);

    const explanation = await screen.findByTestId("bills-owner-only");
    expect(explanation).toHaveTextContent("Owner only");
    expect(explanation).toHaveTextContent("Bills is visible to the household owner.");
    expect(screen.queryByTestId("bills-load-error")).not.toBeInTheDocument();
  });

  it("a non-403 failure renders the generic load error, not the owner-only explanation", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/bills": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    renderWithRouter(<BillsPage />);

    expect(await screen.findByTestId("bills-load-error")).toHaveTextContent("Couldn't load your bills.");
    expect(screen.queryByTestId("bills-owner-only")).not.toBeInTheDocument();
  });
});
