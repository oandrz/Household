// Follows BudgetPage.test.tsx/GoalsPage.test.tsx's own stub-and-provider
// shape: renderWithRouter for a fresh QueryClient, stubFetchRoutes for every
// request (it throws on anything unregistered -- both GET /budgets/{month}
// and GET /goals must be registered on every render here, since the panel
// fires both hooks unconditionally on mount).
//
// fixtures below mirror BudgetPage.test.tsx's own budgetFixture and
// GoalsPage.test.tsx's own goalsFixture -- not imported from those files
// (neither exports its own local fixture, the same "no shared test fixture
// module" convention every feature test file in this codebase already
// follows) but built to the identical shape so a real backend response would
// parse against the exact same schema this file's fixtures do.
import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { MoneyCheckInPanel } from "./MoneyCheckInPanel";
import type { BudgetMonthResponse } from "../money/budgetSchemas";
import type { GoalsResponse } from "../money/goalSchemas";

const MONTH = "2026-07";

// The design's own set-state numbers (Household Dashboard.dc.html's Budget
// screen), the same sample BudgetPage.test.tsx's own budgetFixture draws
// from: S$5,200 budgeted, S$3,420 spent, S$1,780 remaining, 66% used.
function budgetFixture(overrides: Partial<BudgetMonthResponse> = {}): BudgetMonthResponse {
  return {
    currency: "SGD",
    month: MONTH,
    budget: { expectedIncomeMinor: 500000, lines: [] },
    categories: [],
    budgetedMinor: 520000,
    spentMinor: 342000,
    remainingMinor: 178000,
    percentUsed: 66,
    percentOk: true,
    daysLeft: 13,
    dailyPaceMinor: 13700,
    dailyPaceOk: true,
    byPerson: [],
    excludedNoRate: 0,
    overCount: 0,
    rolledOverAt: null,
    rolloverGoalId: null,
    rolloverAmountMinor: null,
    ...overrides,
  };
}

// A month with no budget row at all -- budgetMonthResponseSchema's own
// comment: every figure beside `budget` is still real (BudgetService.Month
// keeps answering them) even when `budget` itself is null, so percentOk/
// dailyPaceOk both come back false rather than merely absent.
const NO_BUDGET: BudgetMonthResponse = budgetFixture({
  budget: null,
  percentOk: false,
  percentUsed: 0,
  dailyPaceOk: false,
  budgetedMinor: 0,
  spentMinor: 0,
  remainingMinor: 0,
});

function goalsFixture(summaryOverrides: Partial<GoalsResponse["summary"]> = {}): GoalsResponse {
  return {
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
      ...summaryOverrides,
    },
  };
}

// useBudget.ts's own second query: it fetches the PREVIOUS month too,
// enabled only while the current month's own `budget` resolves to null
// (BudgetPage's "Import last month" card, which this panel never renders --
// but the hook fires the request regardless of who's calling it). Every
// no-budget test below therefore needs this route registered too, or
// stubFetchRoutes throws inside that second query -- caught internally by
// TanStack Query as that query's own isolated error (MoneyCheckInPanel never
// reads `prevMonthHasBudget`, so nothing here would visibly fail without
// this), but an unregistered route a component genuinely calls is exactly
// what this codebase's own convention says to register, not lean on being
// silently absorbed (Task 13's own "an unregistered route throws before any
// capture runs" finding). Registered unconditionally in renderPanel's own
// defaults rather than per-test: harmless when the current month has a real
// budget (the query stays disabled and this stub is simply never hit).
const PREV_MONTH = "2026-06";

function renderPanel(
  { budget, goals }: { budget: BudgetMonthResponse | null; goals: GoalsResponse },
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budget ?? NO_BUDGET },
    [`GET /api/v1/budgets/${PREV_MONTH}`]: { status: 200, body: { ...NO_BUDGET, month: PREV_MONTH } },
    "GET /api/v1/goals": { status: 200, body: goals },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<MoneyCheckInPanel month={MONTH} />) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("MoneyCheckInPanel", () => {
  // The panel is a prompt, not a record (design spec decision 3): budget
  // figures are the retro's own month, goals are today's live standing, and
  // the panel says which is which rather than let two differently-scoped
  // numbers sit side by side looking like they describe the same thing.
  it("labels the goals line as today's standing, not the month's", async () => {
    renderPanel({ budget: budgetFixture({ percentUsed: 66 }), goals: goalsFixture({ onTrackCount: 4, datedCount: 4 }) });

    expect(await screen.findByTestId("checkin-budget")).toHaveTextContent("66% used");
    expect(screen.getByTestId("checkin-goals")).toHaveTextContent("4 of 4 on track");
    expect(screen.getByTestId("checkin-goals")).toHaveTextContent(/today/i);
  });

  // Budget's own onPaceToSave clause rides along with percentUsed once
  // dailyPaceOk is true -- both are server-computed, both are rendered
  // verbatim (BUDGET_COPY's own strings), nothing here recomputes either.
  it("shows the on-pace-to-save figure alongside percent used", async () => {
    renderPanel({
      budget: budgetFixture({ percentUsed: 66, dailyPaceOk: true, remainingMinor: 178000 }),
      goals: goalsFixture({ onTrackCount: 4, datedCount: 4 }),
    });

    expect(await screen.findByTestId("checkin-budget")).toHaveTextContent("On pace to save SGD 1,780.00");
  });

  // A month with no budget shows Budget's own empty copy, never zeros --
  // this feature has already shipped four "renders a zero" defects (Tasks
  // 10-12's own "0 done"/"0 actions"/empty meta/empty notes), so this test
  // pins the absent case explicitly rather than only the present one. See
  // this task's own report for the mutation that proves it (making the panel
  // render "0% used" here goes red).
  it("says there is no budget rather than showing 0% used", async () => {
    renderPanel({ budget: null, goals: goalsFixture({ onTrackCount: 0, datedCount: 0 }) });

    expect(await screen.findByTestId("checkin-budget")).not.toHaveTextContent("0%");
    expect(screen.getByTestId("checkin-budget")).toHaveTextContent(/No budget set/i);
  });

  // GoalsPage.tsx's own formulas-table rule (datedCount === 0 hides "N of M
  // on track" rather than rendering "0 of 0") applies here too -- a
  // household with no live goals at all yet gets an honest "nothing to check
  // on" sentence, not a colon with nothing after it.
  it("says there are no goals to check on rather than rendering 0 of 0", async () => {
    renderPanel({ budget: budgetFixture(), goals: goalsFixture({ onTrackCount: 0, datedCount: 0, noDateCount: 0 }) });

    expect(await screen.findByTestId("checkin-goals")).not.toHaveTextContent("0 of 0");
    expect(screen.getByTestId("checkin-goals")).toHaveTextContent(/no goals/i);
  });

  // useBudget(month)/useGoals() are both owner-gated on the server -- a
  // household owner who holds marriage but not money capability reaches
  // this exact failure the moment the modal opens. The panel says so inline
  // rather than throwing, and (implicitly, since nothing here renders a
  // form) never blocks anything else in the modal around it.
  it("shows a load error rather than crashing when the budget fetch fails", async () => {
    stubFetchRoutes({
      [`GET /api/v1/budgets/${MONTH}`]: { status: 403, body: { error: { code: "FORBIDDEN", message: "no" } } },
      "GET /api/v1/goals": { status: 200, body: goalsFixture() },
    });
    renderWithRouter(<MoneyCheckInPanel month={MONTH} />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load this month's numbers.");
  });
});
