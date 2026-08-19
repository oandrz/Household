import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { currentMonth } from "../money/month";
import type { RetrosResponse } from "../marriage/retroSchemas";
import { OVERVIEW_COPY } from "./copy";
import { OverviewPage } from "./OverviewPage";

const MONTH = currentMonth();

function meBody(overrides: { role?: string; capabilities?: string[] } = {}) {
  return {
    user: { id: "u1", email: "sam@newhouse.test", displayName: "Sam", avatarInitial: "S" },
    household: {
      id: "h1",
      name: "Rivera household",
      familyName: "Rivera",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "m1",
      householdId: "h1",
      userId: "u1",
      role: overrides.role ?? "owner",
      capabilities: overrides.capabilities ?? ["calendar", "chores", "money", "marriage"],
    },
    capabilities: overrides.capabilities ?? ["calendar", "chores", "money", "marriage"],
    spaces: [],
  };
}

// A computable summary: the shape features/money/schemas.ts's discriminated
// union takes when the server could convert every account.
function summaryBody(netWorthMinor: number, trend?: unknown) {
  return {
    computable: true,
    currency: "SGD",
    netWorthMinor,
    assetsMinor: netWorthMinor,
    liabilitiesMinor: 0,
    breakdown: [],
    excludedNoRate: [],
    excludedByChoice: 0,
    ...(trend === undefined ? {} : { trend }),
  };
}

// Two complete months is all the card needs: it draws no chart, only the
// percentage between the newest month and the one before it.
function trendBody(changeBasisPoints: number) {
  const months = [
    "2025-08", "2025-09", "2025-10", "2025-11",
    "2025-12", "2026-01", "2026-02", "2026-03",
    "2026-04", "2026-05", "2026-06", "2026-07",
  ];
  return {
    points: months.map((month, index) => ({
      month,
      netWorthMinor: 1200000 + index * 4000,
      complete: true,
    })),
    changeBasisPoints,
  };
}

// Spelled out here rather than imported from TransactionsPage.test.tsx -- a
// test file that reaches into another feature's fixtures breaks when that
// feature's own tests change for reasons of their own.
const ACCOUNT = {
  id: "a1",
  nickname: "DBS Everyday",
  type: "cash",
  ownerMembershipId: null,
  ownerName: null,
  balance: { amountMinor: 500000, currency: "SGD" },
  openingBalance: { amountMinor: 400000, currency: "SGD" },
  balanceAsOf: "2026-07-01",
  countTowardNetWorth: true,
  visibleToLimitedMembers: false,
  archivedAt: null,
};

function budgetBody(overrides: Record<string, unknown> = {}) {
  return {
    currency: "SGD",
    month: MONTH,
    budget: { expectedIncomeMinor: null, lines: [] },
    categories: [],
    budgetedMinor: 200000,
    spentMinor: 124000,
    remainingMinor: 76000,
    percentUsed: 62,
    percentOk: true,
    daysLeft: 2,
    dailyPaceMinor: 0,
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

// A minimal, valid live goal -- goalsResponseSchema.parse (useGoals.ts's own
// fetchGoals) runs on whatever `goalsBody` answers here, unlike
// GoalsCard.test.tsx's own fixture, which hands GoalsCard already-typed props
// directly and skips the wire round trip. Every field below is required by
// goalSchema even though this suite never reads most of them.
function goalStub(overrides: Record<string, unknown> = {}) {
  return {
    id: "goal-1",
    name: "Bali",
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
    ...overrides,
  };
}

// A goals response with no live goals at all -- the default for every test
// below that doesn't care about GoalsCard's own figures (GoalsCard.test.tsx
// covers those in isolation). `summaryOverrides` nests rather than
// spreading flat like budgetBody's own overrides, because everything the
// card's "X of Y"/"next"/"with no date" clauses read lives inside `summary`.
// `goals` defaults to empty too -- but GoalsCard.tsx's own empty-state check
// (`goals.goals.length > 0`, not a sum of summary counts, after the fix an
// achieved-goal review round found: an achieved goal is in neither
// `datedCount` nor `noDateCount`) means any test wanting the card to show
// real content must pass a non-empty `goals` array here, not just non-zero
// summary counts.
function goalsBody(summaryOverrides: Record<string, unknown> = {}, goals: Record<string, unknown>[] = []) {
  return {
    currency: "SGD",
    goals,
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

// A bills response with no live bills at all (billsResponseSchema's own
// shape) -- the default for every owner-role test below that doesn't care
// about NextBillCard's own figures (NextBillCard.test.tsx covers those in
// isolation). NextBillCard.tsx owns its own useBills call and mounts
// unconditionally for an owner, so every owner-role test in this file needs
// this route registered even when it never asserts on the card itself --
// stubFetchRoutes throws on an unregistered request.
function billsBody(summaryOverrides: Record<string, unknown> = {}) {
  return {
    bills: [],
    paidThisMonth: [],
    summary: {
      currency: "SGD",
      dueThisMonthMinor: 0,
      paidSoFarMinor: 0,
      nextDue: null,
      autopayCount: 0,
      billCount: 0,
      subscriptionsMonthlyMinor: 0,
      subscriptionsAnnualMinor: 0,
      excludedNoRate: 0,
      ...summaryOverrides,
    },
  };
}

// A retros response with no retros at all and nothing startable
// (retrosResponseSchema's own shape) -- the default for every owner-role
// test below that doesn't care about NextRetroCard's own figures
// (NextRetroCard.test.tsx covers those in isolation). meBody()'s own default
// capabilities list includes "marriage", so every owner-role test in this
// file needs this route registered even when it never asserts on the card
// itself -- stubFetchRoutes throws on an unregistered request, the same
// reason billsBody()'s own comment above gives for NextBillCard.
function retrosBody(overrides: Partial<RetrosResponse> = {}): RetrosResponse {
  return {
    retros: [],
    mood: [],
    doneCount: 0,
    since: null,
    startMonth: null,
    ...overrides,
  };
}

// A retros response that WOULD render NextRetroCard's own content if the
// query behind it were ever allowed to fire -- used only by the two
// marriage-gate tests below, which need to tell "the gate correctly kept
// the request from firing" apart from "the request fired, errored, and the
// card's own null-on-no-data render happened to swallow it" (the exact trap
// this task's own brief names: an unregistered route's throw, or a
// component's own catch, can make an absence assertion pass for the wrong
// reason). A route registered with real, renderable data closes that gap --
// removing the gate in the mutation check below makes the card's own
// content actually appear, not merely "no card, for some reason or other."
function renderableRetrosBody(): RetrosResponse {
  return retrosBody({
    retros: [{ id: "retro-1", month: MONTH, mood: null, actionCount: 1, openActionCount: 1, quote: "", finished: false }],
  });
}

// `routes` accepts a single response or an ordered list per route (the same
// union GoalModal.test.tsx's own `renderModal` widens `extraRoutes` to) --
// this task's own "refetches goals after it saves" test needs a route that
// answers differently across two calls, the same shape useGoals.test.ts's
// goalsResponse/goalsResponseAfterWrite pair proves a mutation's invalidate
// actually refetches.
//
// Returns `fetchMock` too (BudgetPage.test.tsx's own
// "does not fetch budget history" test is the precedent for this exact
// shape) -- Task 16's own limited-member tests need to prove GET /bills was
// never *called*, which is a stronger claim than any card's absence: a test
// asserting only that a heading is missing would stay green even if the
// query fired and merely errored quietly in the background.
function renderOverview(routes: Record<string, RouteResponse | RouteResponse[]>) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": {
      status: 200,
      body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
    },
    "GET /api/v1/household/members": { status: 200, body: [] },
    ...routes,
  });
  return { fetchMock, ...renderWithRouter(<OverviewPage />) };
}

describe("OverviewPage", () => {
  it("shows net worth, this month's budget, the next bill and goals on track to an owner", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(1248000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/bills": {
        status: 200,
        body: billsBody({
          billCount: 1,
          nextDue: {
            billId: "bill-1",
            billName: "SP utilities",
            dueOn: "2026-07-08",
            amountMinor: 14230,
            currency: "SGD",
            overdue: false,
            autopay: true,
          },
        }),
      },
      "GET /api/v1/goals": {
        status: 200,
        body: goalsBody(
          {
            onTrackCount: 3,
            datedCount: 4,
            nextGoal: { id: "goal-1", name: "Bali", targetMonth: "2026-12" },
          },
          [goalStub()],
        ),
      },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    expect(await screen.findByText("S$12,480.00")).toBeInTheDocument();
    expect(await screen.findByText("62% used")).toBeInTheDocument();
    expect(await screen.findByText("S$142.30")).toBeInTheDocument();
    expect(screen.getByText("SP utilities · Jul 8")).toBeInTheDocument();
    expect(await screen.findByText("3 of 4")).toBeInTheDocument();
    expect(screen.getByText("next: Bali · Dec 2026")).toBeInTheDocument();
  });

  it("shows the change on the net worth card, and never the chart", async () => {
    const { container } = renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": {
        status: 200,
        body: { accounts: [], summary: summaryBody(1248000, trendBody(210)) },
      },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/bills": {
        status: 200,
        body: billsBody({
          billCount: 1,
          nextDue: {
            billId: "bill-1",
            billName: "SP utilities",
            dueOn: "2026-07-08",
            amountMinor: 14230,
            currency: "SGD",
            overdue: false,
            autopay: true,
          },
        }),
      },
      "GET /api/v1/goals": {
        status: 200,
        body: goalsBody(
          {
            onTrackCount: 3,
            datedCount: 4,
            nextGoal: { id: "goal-1", name: "Bali", targetMonth: "2026-12" },
          },
          [goalStub()],
        ),
      },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    expect(await screen.findByText("▲ 2.1% this month")).toBeInTheDocument();
    // The design draws no chart here, and the card must not grow one just
    // because the data to draw it arrived.
    expect(container.querySelector("[data-testid='net-worth-bar']")).toBeNull();
  });

  it("tells a member without money that Money is not shared with them", async () => {
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores"] }),
      },
      "GET /api/v1/accounts": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Not allowed." } },
      },
    });

    expect(await screen.findByText(/don't have access to money/i)).toBeInTheDocument();
    // Not an error state, and not a zero -- zero would be a claim about this
    // household's money.
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("never asks for a budget, goals or bills on behalf of a limited member, who cannot read any of them", async () => {
    // GET /budgets/{month}, GET /goals and GET /bills are all
    // requireCapability(money) AND requireOwner (router.go). A limited
    // member with money can see account names and nothing else -- rendering
    // any of these three would be rendering a card that can only ever 403.
    //
    // The assertion is that none of these requests is ever *made*, not
    // merely that none of their cards appears. Absence of a card is the
    // weaker claim: it holds for several reasons at once (the render guard,
    // the `enabled` gate, or simply no data having arrived), so a test
    // asserting only that stays green when either guard is deleted. This
    // one goes red the moment `enabled: isOwner` stops gating any of the
    // three queries -- which is the guard that keeps a doomed 403 out of
    // the cache. useGoals' own `enabled: false` idle path (Task 10's own
    // gap: nothing exercised it until this page became its first real
    // consumer) is what this pins for goals.
    //
    // Bills gets the stricter proof of the three: no route registered for
    // it at all, so a wrongly-enabled query fails this test the moment it
    // fires (stubFetchRoutes' own throw-on-unregistered-request behaviour),
    // rather than merely leaving a `billsRequested` flag unset the way a
    // registered-but-uncalled route would -- task-16-brief.md's own
    // instruction for exactly this test.
    let budgetRequested = false;
    let goalsRequested = false;
    const { fetchMock } = renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores", "money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
      [`GET /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: budgetBody(),
        capture: () => {
          budgetRequested = true;
        },
      },
      "GET /api/v1/goals": {
        status: 200,
        body: goalsBody(),
        capture: () => {
          goalsRequested = true;
        },
      },
      // No "GET /api/v1/bills" entry -- deliberate, see comment above.
    });

    await screen.findByText("Overview");
    expect(budgetRequested).toBe(false);
    expect(goalsRequested).toBe(false);
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/api/v1/bills"))).toBe(false);
    expect(screen.queryByText("This month")).toBeNull();
    expect(screen.queryByText(OVERVIEW_COPY.goalsHeading)).toBeNull();
    expect(screen.queryByText(OVERVIEW_COPY.nextBillHeading)).toBeNull();
  });

  // Found in a browser, not here: every assertion below passed against a page
  // that rendered the word "Overview" and nothing else. A limited member with
  // money gets no summary (the server omits it), no budget card and no
  // checklist, so all three earlier tests' absence-assertions held while the
  // page itself was blank. A page with nothing on it reads as broken rather
  // than as restricted -- the same rule SYSTEM_DESIGN §4 states for the
  // ledger and the budget screen.
  //
  // Extended here, not duplicated into a new test, to add the goals card and
  // the quick-add entry to what this member must not see: a *new* test built
  // only from `queryByText(...)`/`queryByRole(...)` absence checks would be
  // exactly the shape that let the original defect through everywhere else
  // in this file -- absence holds perfectly over a blank page. This test
  // stays trustworthy because its positive assertion (the real message
  // below) proves the page rendered *something*, so the absence checks that
  // follow it mean "guarded," not "nothing loaded yet."
  it("explains the missing figures to a limited member rather than showing them an empty page", async () => {
    const { fetchMock } = renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["money"] }),
      },
      // No `summary` key at all -- the shape the server actually returns to a
      // caller who may not see amounts.
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
      // No "GET /api/v1/bills" entry either -- this member is `!isOwner`,
      // so NextBillCard.tsx's own useBills call must stay disabled here too
      // (task-16-brief.md's own instruction: register no route, so a query
      // that fires anyway fails loudly rather than passing quietly).
    });

    expect(await screen.findByText(/amounts are hidden/i)).toBeInTheDocument();
    // Neither the goals card, the next-bill card, nor the entry that
    // creates either: this member is `!isOwner`, the same gate that keeps
    // the budget card and "+ Add" away from them already.
    expect(screen.queryByText(OVERVIEW_COPY.goalsHeading)).toBeNull();
    expect(screen.queryByText(OVERVIEW_COPY.nextBillHeading)).toBeNull();
    expect(screen.queryByRole("button", { name: "+ Add" })).toBeNull();
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/api/v1/bills"))).toBe(false);
  });

  it("offers a way to set one when the household has never budgeted", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: budgetBody({ budget: null, budgetedMinor: 0, spentMinor: 0, percentUsed: 0 }),
      },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    const link = await screen.findByRole("link", { name: /set a budget/i });
    expect(link).toHaveAttribute("href", "/money/budget");
  });

  it("shows a fresh household what is left to set up", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: budgetBody({ budget: null, budgetedMinor: 0, spentMinor: 0, percentUsed: 0 }),
      },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    expect(await screen.findByText("Finish setting up")).toBeInTheDocument();
    expect(screen.getByText("1 of 3 done")).toBeInTheDocument();

    // Each unfinished step's own link, not just the count: a checklist that
    // shows the right number of steps and sends you to the wrong screen is
    // worse than no checklist. The account step is the one the walk reached
    // through "+ Add" instead, so nothing else covers it.
    const [accountStep, budgetStep] = screen.getAllByRole("link", { name: "Set up" });
    expect(accountStep).toHaveAttribute("href", "/money");
    expect(budgetStep).toHaveAttribute("href", "/money/budget");
  });

  it("drops the checklist once the household has finished setting up", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [ACCOUNT],
          summary: summaryBody(500000),
        },
      },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    // Waiting on the budget card, not merely on the heading: the checklist's
    // last step reads the budget, so asserting its absence before that query
    // resolves would pass against a page that simply had not finished
    // loading yet.
    await screen.findByText("62% used");
    expect(screen.queryByText("Finish setting up")).toBeNull();
  });

  // The sibling of the blank-page defect above, and the same root cause:
  // deriving a claim from data that has not arrived. `hasAccount` and
  // `hasBudget` are both false while their queries are in flight, which is
  // indistinguishable from "this household has neither" -- so an established
  // household was told to go and set up the account and budget it already has,
  // on every cold load of the app's front door, until the figures landed.
  //
  // Asserted on the synchronous first render rather than through a timer: at
  // that moment no query has resolved, which is exactly the state the flash
  // happens in, and it needs no fake clock to observe.
  it("shows no checklist before the data it reads has arrived", async () => {
    // The window that matters is not the first render -- `me` has not resolved
    // then either, so nothing owner-only is on screen at all. It is the render
    // *after* `me` lands and before the money queries do. Held open here by
    // delaying only those two responses, so the assertion lands inside it
    // deterministically rather than by racing a timer.
    const routed = stubFetchRoutes({
      "GET /api/v1/currencies": {
        status: 200,
        body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
      },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [ACCOUNT], summary: summaryBody(500000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/accounts") || url.includes("/budgets")) {
        await new Promise((r) => setTimeout(r, 50));
      }
      return routed(input, init);
    });

    renderWithRouter(<OverviewPage />);

    // "+ Add" appears as soon as `me` resolves, so this is the moment the
    // household is known to be an owner and its figures are still in flight.
    await screen.findByRole("button", { name: "+ Add" });
    expect(screen.queryByText("Finish setting up")).toBeNull();

    // And it stays absent once the data confirms this household is set up.
    expect(await screen.findByText("62% used")).toBeInTheDocument();
    expect(screen.queryByText("Finish setting up")).toBeNull();
  });

  it("keeps the checklist away from a limited member, who cannot do any of it", async () => {
    // Writing a budget is requireOwner, and so is adding an account.
    // Offering the steps would be offering work they cannot do.
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores", "money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
    });

    await screen.findByText("Overview");
    expect(screen.queryByText("Finish setting up")).toBeNull();
  });

  it("offers only the things it can actually create", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));

    expect(screen.getByRole("button", { name: "Account" })).toBeInTheDocument();
    // Savings goal and Bill both joined Transaction and Account before this
    // task's own brief (Bill is this task's own addition). Calendar event
    // and Marriage retro are still in the design and do not exist yet -- a
    // row that does nothing reads as broken.
    expect(screen.getByRole("button", { name: "Savings goal" })).toBeInTheDocument();
    // Exact name, not a /bill/i regex: NextBillCard's own empty state
    // renders an "Add a bill" link (role "link") when this household has no
    // bills yet, and a looser match here would coincidentally pass against
    // that text too.
    expect(screen.getByRole("button", { name: "Bill" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /calendar event/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /marriage retro/i })).toBeNull();
  });

  it("does not offer Transaction or Bill before there is an account to attach them to", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));

    expect(screen.getByRole("button", { name: "Transaction" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Bill" })).toBeDisabled();
    // Both entries carry the identical precondition -- a bill needs a
    // pay-from account exactly as a transaction needs one to post against --
    // so the reason appears once beside each button, not merged into one
    // shared line neither button actually owns.
    expect(screen.getAllByText("Add an account first")).toHaveLength(2);
    // Savings goal has no such precondition -- decision 6, spec's own line:
    // contributions move no real money, so unlike Transaction/Bill there is
    // no account either entry depends on existing first.
    expect(screen.getByRole("button", { name: "Savings goal" })).toBeEnabled();
  });

  it("gives a limited member no quick-add at all", async () => {
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores", "money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
    });

    await screen.findByText("Overview");
    expect(screen.queryByRole("button", { name: "+ Add" })).toBeNull();
  });

  // The modals the menu opens carry their own queries -- TransactionModal's
  // useCategories in particular. Mounting them alongside the menu would fire
  // those on every visit to the app's most-visited page, which is the exact
  // regression TransactionsPage.tsx's own conditional mount is commented to
  // prevent. stubFetchRoutes throws on an unregistered request, so a request
  // fired before anyone opens a modal fails this test rather than passing
  // quietly.
  it("does not fetch a modal's data until that modal is opened", async () => {
    let categoriesRequested = false;
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [ACCOUNT], summary: summaryBody(500000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/categories": {
        status: 200,
        body: { categories: [] },
        capture: () => {
          categoriesRequested = true;
        },
      },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));
    expect(categoriesRequested).toBe(false);
  });

  // GoalModal.tsx calls useGoals({ enabled: false }) itself (its own header
  // comment: it never reads the list, only writes to it), so this menu's own
  // GET /goals is the page-level `useGoals({ enabled: isOwner })` above,
  // already mounted and active by the time anyone opens this modal. On save,
  // createGoal's onSuccess invalidates that same query (useGoals.ts's own
  // invalidateGoals) -- this test watches the figure that invalidation
  // produces actually move, not merely that a second request happened, the
  // same standard every other test in this file (and CLAUDE.md's own "watch
  // the numbers actually move" rule) holds elsewhere.
  it("opens the goal modal from + Add and refetches goals once it saves", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/goals": [
        { status: 200, body: goalsBody() },
        {
          status: 200,
          body: goalsBody(
            {
              onTrackCount: 1,
              datedCount: 1,
              nextGoal: { id: "goal-1", name: "Japan 2027", targetMonth: "2026-12" },
            },
            [goalStub({ id: "goal-1", name: "Japan 2027" })],
          ),
        },
      ],
      "POST /api/v1/goals": {
        status: 201,
        body: {
          goal: {
            id: "goal-1",
            name: "Japan 2027",
            targetMinor: 1000000,
            currency: "SGD",
            targetMonth: "2026-12",
            plannedMonthlyMinor: 200000,
            contributedMinor: 0,
            percent: 0,
            status: "on_track",
            requiredMonthlyMinor: 200000,
            requiredMonthlyOk: true,
            archivedAt: null,
          },
        },
      },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    expect(await screen.findByText(OVERVIEW_COPY.goalsNone)).toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));
    fireEvent.click(screen.getByRole("button", { name: "Savings goal" }));

    await screen.findByLabelText("Goal name");
    fireEvent.change(screen.getByLabelText("Goal name"), { target: { value: "Japan 2027" } });
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000.00" } });
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-12" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Planned each month"), { target: { value: "2000.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByText("1 of 1")).toBeInTheDocument();
    expect(screen.getByText("next: Japan 2027 · Dec 2026")).toBeInTheDocument();
    expect(screen.queryByLabelText("Goal name")).not.toBeInTheDocument();
  });

  // BillModal.tsx calls useAccounts/useCategories/useHouseholdMembers
  // itself (its own header comment), so nothing here passes it props the
  // way TransactionModal above needs -- gating on `billOpen` alone is
  // enough. On save, createBill's onSuccess invalidates both
  // billsQueryKey(false)/billsQueryKey(true) (useBills.ts's own
  // invalidateBills) -- this test watches the figure that invalidation
  // produces actually move, the same standard the goal-modal test above
  // holds itself to, not merely that a second request happened.
  it("opens the bill modal from + Add and moves the next-bill card once it saves", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [ACCOUNT], summary: summaryBody(500000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/categories": {
        status: 200,
        body: { categories: [{ id: "cat-1", name: "Utilities", kind: "expense" }] },
      },
      "GET /api/v1/bills": [
        { status: 200, body: billsBody() },
        {
          status: 200,
          body: billsBody({
            billCount: 1,
            nextDue: {
              billId: "bill-1",
              billName: "SP utilities",
              dueOn: "2026-08-20",
              amountMinor: 14230,
              currency: "SGD",
              overdue: false,
              autopay: false,
            },
          }),
        },
      ],
      "POST /api/v1/bills": {
        status: 201,
        body: {
          bill: {
            id: "bill-1",
            name: "SP utilities",
            amountMinor: 14230,
            currency: "SGD",
            cadence: "monthly",
            nextDue: "2026-08-20",
            categoryId: "cat-1",
            categoryName: "Utilities",
            payFromAccountId: "a1",
            accountName: "DBS Everyday",
            paidByMembershipId: "",
            autopay: false,
            isSubscription: false,
            overdue: false,
            dueSoon: true,
            settled: false,
            archivedAt: null,
          },
        },
      },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    expect(await screen.findByText(OVERVIEW_COPY.nextBillNone)).toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));
    fireEvent.click(screen.getByRole("button", { name: "Bill" }));

    await screen.findByLabelText("Bill name");
    fireEvent.change(screen.getByLabelText("Bill name"), { target: { value: "SP utilities" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "142.30" } });
    fireEvent.change(screen.getByLabelText("Next due"), { target: { value: "2026-08-20" } });
    fireEvent.change(screen.getByLabelText("Category"), { target: { value: "cat-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add bill" }));

    expect(await screen.findByText("S$142.30")).toBeInTheDocument();
    expect(screen.getByText("SP utilities · Aug 20")).toBeInTheDocument();
    expect(screen.queryByLabelText("Bill name")).not.toBeInTheDocument();
  });

  // A bill needs a pay-from account exactly as a transaction needs one to
  // post against -- QuickAddMenu.tsx's own canAddBill mirrors
  // canAddTransaction for this reason. Distinct from the "does not offer"
  // test above: that one proves the button is disabled and explained
  // *before* any account exists; this one proves the whole tree still
  // renders the design's "S$142.30" line once one does.
  it("offers Bill once an account exists", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [ACCOUNT], summary: summaryBody(500000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));
    expect(screen.getByRole("button", { name: "Bill" })).toBeEnabled();
  });

  // Overview is the one page every member reaches, so NextRetroCard's own
  // ABSENCE for a member without marriage needs a positive test -- an
  // absence assertion alone holds perfectly over a blank page, which is
  // exactly how the interim Overview shipped a limited member a page
  // containing only the word "Overview" (docs/LEARNING.md pattern 2, and
  // the "explains the missing figures" test above, which this test's own
  // positive assertion is modelled on for the identical reason).
  //
  // "GET /api/v1/retros" is registered with real, renderable data
  // (renderableRetrosBody, not an empty one) on purpose: if OverviewPage's
  // own `hasMarriage &&` gate were ever deleted, this member's browser
  // would show the card's real content, not merely "no card" for some
  // unrelated reason (an errored, unregistered request that NextRetroCard's
  // own `if (!retros.data) return null` swallows into an identical-looking
  // absence). See the mutation check below.
  it("renders nothing for a member without the marriage capability, and the rest of the page still renders", async () => {
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ capabilities: ["money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(1248000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: renderableRetrosBody() },
    });

    // The rest of the page is alive -- an owner with money still gets their
    // real net worth figure.
    expect(await screen.findByText("S$12,480.00")).toBeInTheDocument();
    expect(screen.queryByTestId("next-retro-card")).not.toBeInTheDocument();
  });

  // The sibling proof the gate test above cannot give on its own: not just
  // that the card is missing, but that the browser never even asked. A
  // member without marriage whose GET /retros somehow fired anyway would
  // cost a doomed 403 (or, as here, a wasted 200) on every single visit to
  // this page -- the app's most-visited screen. Registered (not left
  // unregistered) and tracked with `capture`, per this task's own global
  // instruction: an unregistered route's throw can be swallowed by
  // TanStack Query's error path or a component's own catch with the suite
  // staying green, so this asserts the request directly rather than
  // leaning on that throw.
  it("makes no request for a member who cannot see retros", async () => {
    let retrosRequested = false;
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ capabilities: ["money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": {
        status: 200,
        body: renderableRetrosBody(),
        capture: () => {
          retrosRequested = true;
        },
      },
    });

    await screen.findByText("Overview");
    expect(retrosRequested).toBe(false);
  });
});
