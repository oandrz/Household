import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { currentMonth } from "../money/month";
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
    ...overrides,
  };
}

function renderOverview(routes: Record<string, RouteResponse>) {
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
  it("shows net worth and this month's budget to an owner", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(1248000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
    });

    expect(await screen.findByText("S$12,480.00")).toBeInTheDocument();
    expect(await screen.findByText("62% used")).toBeInTheDocument();
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

  it("never asks for a budget on behalf of a limited member, who cannot read one", async () => {
    // GET /budgets/{month} is requireCapability(money) AND requireOwner
    // (router.go). A limited member with money can see account names and
    // nothing else -- rendering a budget card that can only 403 would be a
    // card that is always broken.
    //
    // The assertion is that the request is never *made*, not merely that no
    // card appears. Absence of the card is the weaker claim: it holds for
    // several reasons at once (the render guard, the `enabled` gate, or
    // simply no data having arrived), so a test asserting only that stays
    // green when either guard is deleted. This one goes red the moment
    // `enabled: isOwner` stops gating the query -- which is the guard that
    // keeps a doomed 403 out of the cache.
    let budgetRequested = false;
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
    });

    await screen.findByText("Overview");
    expect(budgetRequested).toBe(false);
    expect(screen.queryByText("This month")).toBeNull();
  });

  it("offers a way to set one when the household has never budgeted", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: budgetBody({ budget: null, budgetedMinor: 0, spentMinor: 0, percentUsed: 0 }),
      },
    });

    const link = await screen.findByRole("link", { name: /set a budget/i });
    expect(link).toHaveAttribute("href", "/money/budget");
  });
});
