// Follows FinancesPage.test.tsx/TransactionsPage.test.tsx's stub-and-provider
// setup: renderWithRouter (BudgetPage has no <Link> of its own today, but
// the same harness every other money-feature test uses keeps this file
// consistent with its siblings) plus stubFetchRoutes for every request.
//
// BudgetPage derives its initial month from the real calendar
// (currentMonth()), so every test here fakes `Date` to a fixed day in July
// 2026 -- the same pattern AccountModal.test.tsx's own today() tests use --
// so the page always requests "GET /api/v1/budgets/2026-07" and this file
// never has to guess which month the test runner's real clock would land on.
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BudgetPage } from "./BudgetPage";
import type { BudgetMonthResponse } from "./budgetSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

// The design's own set-state numbers (Household Dashboard.dc.html's Budget
// screen), scaled into minor units: S$5,200 budgeted, S$3,420 spent, S$1,780
// remaining, S$137/day, Christine S$1,610, Andreas S$1,240. Dining out is
// the one over-cap category (465 spent against a 450 cap in the design).
function budgetFixture(overrides: Partial<BudgetMonthResponse> = {}): BudgetMonthResponse {
  return {
    currency: "SGD",
    month: "2026-07",
    budget: { expectedIncomeMinor: 500000, lines: [] },
    categories: [
      {
        categoryId: "cat-1",
        name: "Groceries",
        archived: false,
        capMinor: 80000,
        spentMinor: 64000,
        over: false,
      },
      {
        categoryId: "cat-2",
        name: "Dining out",
        archived: false,
        capMinor: 45000,
        spentMinor: 46500,
        over: true,
      },
      {
        categoryId: "cat-3",
        name: "Old hobby",
        archived: true,
        capMinor: 10000,
        spentMinor: 5000,
        over: false,
      },
    ],
    budgetedMinor: 520000,
    spentMinor: 342000,
    remainingMinor: 178000,
    percentUsed: 66,
    percentOk: true,
    daysLeft: 13,
    dailyPaceMinor: 13700,
    dailyPaceOk: true,
    byPerson: [
      { membershipId: "m1", name: "Christine", spentMinor: 161000 },
      { membershipId: "m2", name: "Andreas", spentMinor: 124000 },
    ],
    excludedNoRate: 0,
    overCount: 1,
    ...overrides,
  };
}

function renderPage(
  response: BudgetMonthResponse,
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/budgets/2026-07": { status: 200, body: response },
    ...extraRoutes,
  });
  return renderWithRouter(<BudgetPage />);
}

beforeEach(() => {
  // Only `Date` is faked (`toFake: ["Date"]`) -- setTimeout/setInterval stay
  // real, so `waitFor`/`findBy*`'s own polling still works. Matches
  // AccountModal.test.tsx's own convention for the same reason.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-07-15T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("BudgetPage", () => {
  it("renders the four stat cards from the response's minor units", async () => {
    renderPage(budgetFixture());

    expect(await screen.findByTestId("budget-stat-budgeted")).toHaveTextContent("S$5,200.00");
    expect(screen.getByTestId("budget-stat-spent")).toHaveTextContent("S$3,420.00");
    expect(screen.getByTestId("budget-stat-remaining")).toHaveTextContent("S$1,780.00");
    expect(screen.getByTestId("budget-stat-pace")).toHaveTextContent("S$137.00");
  });

  it("shows the over state and copy on an over-cap category, and names it in the insight card", async () => {
    renderPage(budgetFixture());

    const rows = await screen.findAllByTestId("budget-category-row");
    const diningRow = rows.find((row) => row.textContent?.includes("Dining out"));
    expect(diningRow).toBeDefined();
    expect(diningRow).toHaveTextContent("· over");

    expect(await screen.findByTestId("budget-insight")).toHaveTextContent(
      "Dining out is the only category over.",
    );
  });

  it("shows the count-only over-category sentence when more than one category is over", async () => {
    renderPage(
      budgetFixture({
        overCount: 2,
        categories: [
          budgetFixture().categories[0],
          { ...budgetFixture().categories[1], over: true },
          { ...budgetFixture().categories[2], over: true, archived: false },
        ],
      }),
    );

    expect(await screen.findByTestId("budget-insight")).toHaveTextContent("2 categories are over.");
  });

  it("says 'On pace to save' only when dailyPaceOk", async () => {
    renderPage(budgetFixture());

    expect(await screen.findByTestId("budget-insight")).toHaveTextContent("On pace to save S$1,780.00");
  });

  it("hides the on-pace sentence and the pace stat card when dailyPaceOk is false", async () => {
    renderPage(budgetFixture({ dailyPaceOk: false }));

    await screen.findByTestId("budget-stat-budgeted");
    expect(screen.queryByTestId("budget-stat-pace")).not.toBeInTheDocument();
    expect(screen.queryByText(/On pace to save/)).not.toBeInTheDocument();
  });

  it("hides the percent-used figure when percentOk is false", async () => {
    renderPage(budgetFixture({ percentOk: false }));

    await screen.findByTestId("budget-stat-budgeted");
    expect(screen.queryByTestId("budget-percent-used")).not.toBeInTheDocument();
  });

  it("drops 'so far' language and hides the pace card for a past month (daysLeft: 0)", async () => {
    renderPage(budgetFixture({ daysLeft: 0, dailyPaceOk: false }));

    const spentCard = await screen.findByTestId("budget-stat-spent");
    expect(spentCard).toHaveTextContent("Spent");
    expect(spentCard).not.toHaveTextContent("so far");
    expect(screen.queryByTestId("budget-stat-pace")).not.toBeInTheDocument();
    expect(screen.queryByText(/On pace to save/)).not.toBeInTheDocument();
  });

  // Spec screen state 4: "the header still names the month." A past month
  // that still has a real budget (percentOk true, but daysLeft 0) is the one
  // combination where the percent figure and the days-left phrase (which
  // itself embeds the month name) both go quiet at once -- pinned here
  // because that specific combination once left the header a bare "Budget"
  // heading with the month name nowhere on the page but the picker chip.
  it("still names the month in the header when percentOk is true and the month has ended", async () => {
    renderPage(budgetFixture({ daysLeft: 0, dailyPaceOk: false }));

    expect(await screen.findByTestId("budget-subtitle")).toHaveTextContent("July 2026");
  });

  it("renders the excluded-no-rate ledger-style line with its count", async () => {
    renderPage(budgetFixture({ excludedNoRate: 3 }));

    expect(await screen.findByTestId("budget-excluded-no-rate")).toHaveTextContent(
      "3 transactions are not counted: no exchange rate.",
    );
  });

  it("marks an archived category's line with an archived marker", async () => {
    renderPage(budgetFixture());

    const rows = await screen.findAllByTestId("budget-category-row");
    const archivedRow = rows.find((row) => row.textContent?.includes("Old hobby"));
    expect(archivedRow).toBeDefined();
    expect(within(archivedRow!).getByTestId("budget-category-archived")).toHaveTextContent("(archived)");
  });

  it("renders spending-by-person rows with names and formatted totals", async () => {
    renderPage(budgetFixture());

    const rows = await screen.findAllByTestId("budget-person-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Christine");
    expect(rows[0]).toHaveTextContent("S$1,610.00");
    expect(rows[1]).toHaveTextContent("Andreas");
    expect(rows[1]).toHaveTextContent("S$1,240.00");
  });

  // Spec decision 1, pinned: rollover is deferred to Goals, whole, and the
  // design's own "Unspent budget rolls into the Bali trip goal at month end"
  // sentence must never come back from a future copy-paste of the design.
  it("never renders a rollover sentence", async () => {
    renderPage(budgetFixture());

    await screen.findByTestId("budget-insight");
    expect(document.body.textContent).not.toMatch(/rolls into/i);
  });

  it("renders a placeholder, not the stat cards, when the month has no budget row yet", async () => {
    renderPage(budgetFixture({ budget: null }));

    expect(await screen.findByTestId("budget-empty-placeholder")).toBeInTheDocument();
    expect(screen.queryByTestId("budget-stat-budgeted")).not.toBeInTheDocument();
  });

  it("surfaces a fetch failure as an alert rather than a blank screen", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/budgets/2026-07": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });
    renderWithRouter(<BudgetPage />);

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
  });
});
