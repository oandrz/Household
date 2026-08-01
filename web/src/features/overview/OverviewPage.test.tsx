import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { currentMonth } from "../money/month";
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
function summaryBody(netWorthMinor: number) {
  return {
    computable: true,
    currency: "SGD",
    netWorthMinor,
    assetsMinor: netWorthMinor,
    liabilitiesMinor: 0,
    breakdown: [],
    excludedNoRate: [],
    excludedByChoice: 0,
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

// `routes` accepts a single response or an ordered list per route (the same
// union GoalModal.test.tsx's own `renderModal` widens `extraRoutes` to) --
// this task's own "refetches goals after it saves" test needs a route that
// answers differently across two calls, the same shape useGoals.test.ts's
// goalsResponse/goalsResponseAfterWrite pair proves a mutation's invalidate
// actually refetches.
function renderOverview(routes: Record<string, RouteResponse | RouteResponse[]>) {
  stubFetchRoutes({
    "GET /api/v1/currencies": {
      status: 200,
      body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
    },
    "GET /api/v1/household/members": { status: 200, body: [] },
    ...routes,
  });
  return renderWithRouter(<OverviewPage />);
}

describe("OverviewPage", () => {
  it("shows net worth, this month's budget and goals on track to an owner", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(1248000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
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
    });

    expect(await screen.findByText("S$12,480.00")).toBeInTheDocument();
    expect(await screen.findByText("62% used")).toBeInTheDocument();
    expect(await screen.findByText("3 of 4")).toBeInTheDocument();
    expect(screen.getByText("next: Bali · Dec 2026")).toBeInTheDocument();
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

  it("never asks for a budget or goals on behalf of a limited member, who cannot read either", async () => {
    // GET /budgets/{month} and GET /goals are both requireCapability(money)
    // AND requireOwner (router.go). A limited member with money can see
    // account names and nothing else -- rendering either card would be a
    // card that can only ever 403.
    //
    // The assertion is that neither request is ever *made*, not merely that
    // neither card appears. Absence of a card is the weaker claim: it holds
    // for several reasons at once (the render guard, the `enabled` gate, or
    // simply no data having arrived), so a test asserting only that stays
    // green when either guard is deleted. This one goes red the moment
    // `enabled: isOwner` stops gating either query -- which is the guard
    // that keeps a doomed 403 out of the cache. useGoals' own `enabled:
    // false` idle path (Task 10's own gap: nothing exercised it until this
    // page became its first real consumer) is what this pins for goals.
    let budgetRequested = false;
    let goalsRequested = false;
    renderOverview({
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
    });

    await screen.findByText("Overview");
    expect(budgetRequested).toBe(false);
    expect(goalsRequested).toBe(false);
    expect(screen.queryByText("This month")).toBeNull();
    expect(screen.queryByText(OVERVIEW_COPY.goalsHeading)).toBeNull();
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
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["money"] }),
      },
      // No `summary` key at all -- the shape the server actually returns to a
      // caller who may not see amounts.
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
    });

    expect(await screen.findByText(/amounts are hidden/i)).toBeInTheDocument();
    // Neither the goals card nor the entry that creates one: this member is
    // `!isOwner`, the same gate that keeps the budget card and "+ Add" away
    // from them already.
    expect(screen.queryByText(OVERVIEW_COPY.goalsHeading)).toBeNull();
    expect(screen.queryByRole("button", { name: "+ Add" })).toBeNull();
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
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));

    expect(screen.getByRole("button", { name: "Account" })).toBeInTheDocument();
    // Savings goal joins Transaction and Account in this change (this task's
    // own brief). Bill, Calendar event and Marriage retro are still in the
    // design and do not exist yet -- a row that does nothing reads as
    // broken.
    expect(screen.getByRole("button", { name: "Savings goal" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /bill/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /calendar event/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /marriage retro/i })).toBeNull();
  });

  it("does not offer Transaction before there is an account to attach it to", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));

    expect(screen.getByRole("button", { name: "Transaction" })).toBeDisabled();
    expect(screen.getByText("Add an account first")).toBeInTheDocument();
    // Savings goal has no such precondition -- decision 6, spec's own line:
    // contributions move no real money, so unlike Transaction there is no
    // account this entry depends on existing first.
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
      "GET /api/v1/categories": {
        status: 200,
        body: { categories: [] },
        capture: () => {
          categoriesRequested = true;
        },
      },
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
});
