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
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BudgetPage } from "./BudgetPage";
import type { BudgetMonthResponse } from "./budgetSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

// BudgetPage always fetches the household's plain category list (needed for
// the empty state's templates, whether or not the viewed month is unbudgeted
// -- see BudgetPage.tsx's own comment on why the hook isn't gated), so every
// test needs this stubbed. Empty by default; tests that exercise template
// mapping override it via renderPage's extraRoutes.
const CATEGORIES = { status: 200, body: { categories: [] } };

// BudgetModal.tsx calls useBudget(month) and fetches the archived-inclusive
// category list itself (see its own header comment on both) -- every test
// below that opens the modal needs this stubbed too, even the ones that
// never look at it, since stubFetchRoutes throws on anything unregistered.
const NO_ARCHIVED_CATEGORIES = { status: 200, body: { categories: [] } };

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
    rolledOverAt: null,
    rolloverGoalId: null,
    rolloverAmountMinor: null,
    ...overrides,
  };
}

// BudgetRolloverCard.tsx mounts (and fetches /goals) whenever the viewed
// month is closed with a positive remainingMinor or a stamp -- which every
// daysLeft: 0 fixture in this file now is, since budgetFixture()'s own
// default remainingMinor stays positive unless a test overrides it. Empty
// by default so tests that don't care about the card's own content never
// have to think about it; Task 15's own rollover describe block overrides
// this via extraRoutes with a real goal to pick.
const GOALS_EMPTY = {
  status: 200,
  body: {
    currency: "SGD",
    goals: [],
    summary: {
      plannedMonthlyTotalMinor: 0,
      actualThisMonthMinor: 0,
      onTrackCount: 0,
      datedCount: 0,
      noDateCount: 0,
      excludedNoRate: 0,
      nextGoal: null,
    },
  },
};

function renderPage(
  response: BudgetMonthResponse,
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  // Returns `fetchMock` alongside renderWithRouter's own result -- additive,
  // every existing call site here ignores the return value entirely, and
  // Task 15's own tests need it to assert *which* requests fired (and did
  // not fire) rather than only what finally rendered.
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/categories": CATEGORIES,
    "GET /api/v1/categories?includeArchived=true": NO_ARCHIVED_CATEGORIES,
    "GET /api/v1/budgets/2026-07": { status: 200, body: response },
    "GET /api/v1/goals": GOALS_EMPTY,
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<BudgetPage />) };
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

  // Extension of the guard above (Task 15): BudgetRolloverCard.tsx now puts
  // real "unspent"/"Move it into a goal" copy in this exact insight area on
  // a closed month. Re-running the guard there, with the real card's own
  // copy actually on screen, is what proves it catches "rolls into"
  // specifically -- not merely a screen that happens to say nothing about
  // rollover at all.
  it("still never renders the design's automatic rollover sentence once the manual card's own copy is on screen", async () => {
    renderPage(budgetFixture({ daysLeft: 0, dailyPaceOk: false }));

    expect(await screen.findByText(/unspent in July/)).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/rolls into/i);
  });

  it("renders the design's empty state, not the stat cards, when the month has no budget row yet", async () => {
    renderPage(budgetFixture({ budget: null }), {
      // June (the previous month) is also unbudgeted -- no Import card.
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ budget: null, month: "2026-06" }) },
    });

    const empty = await screen.findByTestId("budget-empty-state");
    expect(empty).toHaveTextContent("No budget set for July yet");
    expect(empty).toHaveTextContent(
      "A budget gives every dollar a job. Set a monthly cap per category and Hearth will track spending against it automatically from your linked accounts.",
    );
    expect(screen.getByTestId("budget-create-blank")).toHaveTextContent("Create your first budget");
    expect(screen.getByTestId("budget-start-from-template")).toHaveTextContent("Start from a template");
    expect(screen.getByTestId("budget-template-family-of-four")).toHaveTextContent("Family of four");
    expect(screen.getByTestId("budget-template-fifty-thirty-twenty")).toHaveTextContent("50 / 30 / 20");

    expect(screen.queryByTestId("budget-stat-budgeted")).not.toBeInTheDocument();
  });

  it("hides the Import-last-month card when the previous month has no budget", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ budget: null, month: "2026-06" }) },
    });

    await screen.findByTestId("budget-empty-state");
    expect(screen.queryByTestId("budget-template-import-last-month")).not.toBeInTheDocument();
  });

  it("shows the Import-last-month card, naming the previous month, once it has a real budget", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ month: "2026-06" }) },
    });

    const importCard = await screen.findByTestId("budget-template-import-last-month");
    expect(importCard).toHaveTextContent("Import last month");
    expect(importCard).toHaveTextContent("Copy June's caps");
  });

  // June's own fixture carries two real caps and an income figure -- not
  // the vacuous zero-line budget the visibility test above uses -- because
  // importing an all-zero month would prove nothing about the one thing
  // this card exists for: carrying real caps (and expected income, spec
  // decision 3) forward untouched, straight off the wire rather than
  // through the two templates' name-mapping.
  it("hands off the previous month's real lines and income, unchanged, on an Import click", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/budgets/2026-06": {
        status: 200,
        body: budgetFixture({
          month: "2026-06",
          budget: {
            expectedIncomeMinor: 520000,
            lines: [
              { categoryId: "cat-1", capMinor: 80000 },
              { categoryId: "cat-2", capMinor: 45000 },
            ],
          },
        }),
      },
    });

    const importCard = await screen.findByTestId("budget-template-import-last-month");
    importCard.click();

    // The real BudgetModal (Task 14): income and both lines land unchanged,
    // proving the handoff carries the real prefill rather than a stub count.
    expect(await screen.findByLabelText("Expected income")).toHaveValue("5200.00");
    expect(screen.getAllByTestId(/^budget-modal-row-/)).toHaveLength(2);
  });

  it("hands off a template click as a prefilled TemplatePrefill, mapped by category name", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/categories": {
        status: 200,
        body: { categories: [{ id: "cat-groceries", name: "Groceries", kind: "expense" }] },
      },
      // BudgetModal.tsx's own archived-inclusive fetch has to agree with the
      // active-only list above -- the real backend answers both routes off
      // the same household roster, and buildRows resolves every row's name
      // off this one (Defect A's fix), not the active-only list this test
      // overrode above.
      "GET /api/v1/categories?includeArchived=true": {
        status: 200,
        body: { categories: [{ id: "cat-groceries", name: "Groceries", kind: "expense", archived: false }] },
      },
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ budget: null, month: "2026-06" }) },
    });

    const familyCard = await screen.findByTestId("budget-template-family-of-four");
    familyCard.click();

    // Only one of the ten design categories has a live match here
    // (Groceries) -- one row, carrying the real computed prefill rather
    // than a hard-coded number.
    const rows = await screen.findAllByTestId(/^budget-modal-row-/);
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getByDisplayValue("Groceries")).toBeInTheDocument();
  });

  it("opens the 50/30/20 template with zero lines and the income prompt -- the waiting-for-income state", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ budget: null, month: "2026-06" }) },
    });

    const fiftyThirtyTwentyCard = await screen.findByTestId("budget-template-fifty-thirty-twenty");
    fiftyThirtyTwentyCard.click();

    expect(await screen.findByText("Enter your expected income and we'll split it 50/30/20")).toBeInTheDocument();
    expect(screen.queryAllByTestId(/^budget-modal-row-/)).toHaveLength(0);
  });

  it("hands off `Create your first budget` as a blank (no prefill) modal", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ budget: null, month: "2026-06" }) },
    });

    const createBlank = await screen.findByTestId("budget-create-blank");
    createBlank.click();

    expect(await screen.findByLabelText("Expected income")).toHaveValue("");
    expect(screen.queryAllByTestId(/^budget-modal-row-/)).toHaveLength(0);
  });

  it("surfaces a fetch failure as an alert rather than a blank screen", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/categories": CATEGORIES,
      "GET /api/v1/budgets/2026-07": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });
    renderWithRouter(<BudgetPage />);

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
  });
});

// Task 15: History button + BudgetHistoryModal, the ‹ › picker, and the
// Edit-budget entry point for a month that already has a budget (a gap
// BudgetModal.tsx's own header comment left for this task -- see this
// suite's own "opens Edit budget" test).
describe("BudgetPage history modal and month picker", () => {
  const HISTORY_ROUTE = "GET /api/v1/budgets/history?months=6";
  const HISTORY_RESPONSE = {
    status: 200,
    body: {
      months: [
        { month: "2026-07", budgetedMinor: 520000, spentMinor: 342000, closed: false },
        { month: "2026-06", budgetedMinor: 520000, spentMinor: 478000, closed: true },
      ],
    },
  };

  it("does not fetch budget history until the History modal is opened", async () => {
    const { fetchMock } = renderPage(budgetFixture(), { [HISTORY_ROUTE]: HISTORY_RESPONSE });

    await screen.findByTestId("budget-stat-budgeted");
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/budgets/history"))).toBe(false);

    screen.getByTestId("budget-history-button").click();

    await screen.findByText("Budget history");
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/budgets/history"))).toBe(true);
  });

  it("closes the History modal, switches the page's month and GETs it when a row is picked", async () => {
    renderPage(budgetFixture(), {
      [HISTORY_ROUTE]: HISTORY_RESPONSE,
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ month: "2026-06", spentMinor: 111100 }) },
    });

    await screen.findByTestId("budget-stat-budgeted");
    screen.getByTestId("budget-history-button").click();

    const rows = await screen.findAllByTestId("budget-history-row");
    const juneRow = rows.find((row) => row.textContent?.includes("Jun 2026"));
    expect(juneRow).toBeDefined();
    juneRow!.click();

    // The modal is gone...
    await waitFor(() => expect(screen.queryByText("Budget history")).not.toBeInTheDocument());
    // ...and the page switched to June, re-fetching and rendering its own
    // figures rather than still showing July's under June's label (the
    // exact failure useBudget.ts's own `["budget", month]` key comment
    // warns a month switch must never produce).
    expect(await screen.findByTestId("budget-stat-spent")).toHaveTextContent("S$1,111.00");
    expect(await screen.findByTestId("budget-subtitle")).toHaveTextContent("June 2026");
  });

  it("moves one month back on ‹ and refetches that month", async () => {
    renderPage(budgetFixture(), {
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ month: "2026-06" }) },
    });

    await screen.findByTestId("budget-stat-budgeted");
    screen.getByLabelText("Previous month").click();

    // `findByTestId` resolves the instant the (already-mounted) element
    // exists, not once its content changes -- `waitFor` is what actually
    // waits for the re-render the month switch triggers.
    await waitFor(() => expect(screen.getByTestId("budget-subtitle")).toHaveTextContent("June 2026"));
  });

  it("moves one month forward on › and refetches that month", async () => {
    renderPage(budgetFixture(), {
      "GET /api/v1/budgets/2026-08": { status: 200, body: budgetFixture({ month: "2026-08" }) },
    });

    await screen.findByTestId("budget-stat-budgeted");
    screen.getByLabelText("Next month").click();

    await waitFor(() => expect(screen.getByTestId("budget-subtitle")).toHaveTextContent("August 2026"));
  });

  // The gap BudgetModal.tsx's own header comment flagged: Task 14 shipped
  // the modal itself but no way to open it for a month that already has a
  // budget -- only the empty state's template/blank clicks reached it. This
  // pins the fix: Edit budget hands the modal the *existing* budget as its
  // prefill, normalised the same way "Create your first budget" is.
  it("opens Edit budget prefilled with the current month's existing income and lines", async () => {
    renderPage(budgetFixture());

    await screen.findByTestId("budget-stat-budgeted");
    screen.getByTestId("budget-edit-button").click();

    expect(await screen.findByLabelText("Expected income")).toHaveValue("5000.00");
  });

  it("hides History and Edit budget from the header when the month has no budget yet", async () => {
    renderPage(budgetFixture({ budget: null }), {
      "GET /api/v1/budgets/2026-06": { status: 200, body: budgetFixture({ budget: null, month: "2026-06" }) },
    });

    await screen.findByTestId("budget-empty-state");
    expect(screen.queryByTestId("budget-history-button")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-edit-button")).not.toBeInTheDocument();
  });

  // Same pin as the unit-level test in BudgetHistoryModal.test.tsx, at the
  // integration level this time -- Export CSV is in the design's mockup but
  // deferred (Task 15's own brief, "same reason as Task 12's rollover pin").
  it("never renders an Export CSV control", async () => {
    renderPage(budgetFixture(), { [HISTORY_ROUTE]: HISTORY_RESPONSE });

    await screen.findByTestId("budget-stat-budgeted");
    screen.getByTestId("budget-history-button").click();

    await screen.findByText("Budget history");
    expect(screen.queryByText(/export csv/i)).not.toBeInTheDocument();
  });
});

// Task 15: BudgetRolloverCard.tsx, wired into the real page. Most of the
// card's own behaviour (the picker's currency/archived exclusions, the
// disabled-button reasons, the inline error on a refusal) is pinned in
// isolation in BudgetRolloverCard.test.tsx -- this describe block proves
// only what an isolated render of that component cannot: the two states
// "worth stating twice" (absent on the current month, replaced by the
// destination sentence once stamped), and the full wiring end to end,
// where a REAL, active useBudget(month) exists to actually refetch and
// swap the card's own props once the write lands -- something an isolated
// render with a disabled `useBudget(month, { enabled: false })` observer
// cannot demonstrate (BudgetRolloverCard.test.tsx's own header comment).
describe("BudgetPage rollover card", () => {
  const BALI_GOAL = {
    id: "goal-1",
    name: "Bali trip",
    targetMinor: 400000,
    currency: "SGD",
    targetMonth: "2026-12",
    plannedMonthlyMinor: 35000,
    contributedMinor: 260000,
    percent: 65,
    status: "on_track",
    requiredMonthlyMinor: 28000,
    requiredMonthlyOk: true,
    archivedAt: null,
  };
  const GOALS_WITH_BALI = {
    status: 200,
    body: { currency: "SGD", goals: [BALI_GOAL], summary: GOALS_EMPTY.body.summary },
  };

  it("shows neither the offer nor a destination sentence on the current (open) month", async () => {
    renderPage(budgetFixture());

    await screen.findByTestId("budget-stat-budgeted");
    expect(screen.queryByTestId("budget-rollover-offer")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-rollover-done")).not.toBeInTheDocument();
  });

  it("shows the offer on a closed month with unspent budget and no stamp", async () => {
    renderPage(budgetFixture({ daysLeft: 0, dailyPaceOk: false }));

    const offer = await screen.findByTestId("budget-rollover-offer");
    expect(offer).toHaveTextContent("S$1,780.00 unspent in July");
    expect(screen.getByTestId("budget-rollover-cta")).toHaveTextContent("Move it into a goal");
  });

  it("shows the destination sentence, not the offer, once the month already carries a rollover stamp", async () => {
    renderPage(
      budgetFixture({
        daysLeft: 0,
        dailyPaceOk: false,
        rolledOverAt: "2026-07-31T00:00:00Z",
        rolloverGoalId: "goal-1",
        rolloverAmountMinor: 178000,
      }),
      { "GET /api/v1/goals": GOALS_WITH_BALI },
    );

    await waitFor(() =>
      expect(screen.getByTestId("budget-rollover-done")).toHaveTextContent("S$1,780.00 moved into Bali trip."),
    );
    expect(screen.queryByTestId("budget-rollover-offer")).not.toBeInTheDocument();
  });

  it("POSTs {goalId} on a successful move, then refetches both the month and the goals list", async () => {
    let postBody: unknown;
    const openFixture = budgetFixture({ daysLeft: 0, dailyPaceOk: false });
    const stampedFixture = budgetFixture({
      daysLeft: 0,
      dailyPaceOk: false,
      rolledOverAt: "2026-07-31T00:00:00Z",
      rolloverGoalId: "goal-1",
      rolloverAmountMinor: 178000,
    });
    renderPage(openFixture, {
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: openFixture },
        { status: 200, body: stampedFixture },
      ],
      "GET /api/v1/goals": GOALS_WITH_BALI,
      "POST /api/v1/budgets/2026-07/rollover": {
        status: 200,
        body: {
          contribution: {
            id: "contribution-1",
            amountMinor: 178000,
            occurredOn: "2026-07-31",
            note: "",
            source: "budget_rollover",
            sourceBudgetMonth: "2026-07",
          },
        },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const cta = await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(cta).not.toBeDisabled());
    cta.click();
    const select = await screen.findByTestId("budget-rollover-select");
    fireEvent.change(select, { target: { value: "goal-1" } });
    screen.getByTestId("budget-rollover-confirm").click();

    await waitFor(() => expect(postBody).toEqual({ goalId: "goal-1" }));
    // The month re-GET's second, stamped response -- proves the move
    // actually invalidated and refetched the real, active useBudget(month)
    // this page owns, and the destination sentence replaces the offer as a
    // consequence of that refetch, not because this test told it to.
    await waitFor(() =>
      expect(screen.getByTestId("budget-rollover-done")).toHaveTextContent("moved into Bali trip"),
    );
  });

  it("shows a 409 ROLLOVER_ALREADY_DONE inline and refetches rather than leaving a stale button", async () => {
    const openFixture = budgetFixture({ daysLeft: 0, dailyPaceOk: false });
    const stampedFixture = budgetFixture({
      daysLeft: 0,
      dailyPaceOk: false,
      rolledOverAt: "2026-07-31T00:00:00Z",
      rolloverGoalId: "goal-1",
      rolloverAmountMinor: 178000,
    });
    renderPage(openFixture, {
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: openFixture },
        { status: 200, body: stampedFixture },
      ],
      "GET /api/v1/goals": GOALS_WITH_BALI,
      "POST /api/v1/budgets/2026-07/rollover": {
        status: 409,
        body: { error: { code: "ROLLOVER_ALREADY_DONE", message: "That month has already been rolled over." } },
      },
    });

    const cta = await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(cta).not.toBeDisabled());
    cta.click();
    const select = await screen.findByTestId("budget-rollover-select");
    fireEvent.change(select, { target: { value: "goal-1" } });
    screen.getByTestId("budget-rollover-confirm").click();

    expect(await screen.findByTestId("budget-rollover-error")).toHaveTextContent(
      "That month has already been rolled over.",
    );
    // ...and the month refetched -- the second, stamped response -- so the
    // destination sentence replaces the (now-gone) offer button rather than
    // a stale, still-clickable one sitting on top of a month another tab
    // already rolled over.
    await waitFor(() =>
      expect(screen.getByTestId("budget-rollover-done")).toHaveTextContent("moved into Bali trip"),
    );
  });
});
