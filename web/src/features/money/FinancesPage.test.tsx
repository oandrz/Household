import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "../auth/schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { FINANCES_COPY } from "./copy";
import { FinancesPage } from "./FinancesPage";

afterEach(() => vi.unstubAllGlobals());

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

// useMe() is called both by this page (to gate FirstRunPanel's add button)
// and by AccountsPanel (to gate its own "+ Add account"), independently of
// what GET /api/v1/accounts answers -- RequireCapability, the route's real
// ancestor, would have already resolved this same ['me'] query before
// mounting the page, but FinancesPage.test.tsx mounts the page on its own via
// renderWithRouter, so every scenario here registers it explicitly rather
// than relying on stubFetchRoutes's registered-or-throw default to happen to
// not matter.
function meFixture(role: "owner" | "limited"): Me {
  return {
    user: { id: "u1", email: "a@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: {
      id: "h1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "m1",
      householdId: "h1",
      userId: "u1",
      role,
      capabilities: role === "owner" ? ["calendar", "chores", "money", "marriage"] : ["money"],
    },
    capabilities: role === "owner" ? ["calendar", "chores", "money", "marriage"] : ["money"],
    spaces: [],
  };
}

describe("FinancesPage", () => {
  it("shows the first-run panel and no empty cards when there are no accounts", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 0,
            assetsMinor: 0, liabilitiesMinor: 0,
            breakdown: [], excludedNoRate: [], excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText("Nothing here yet.")).toBeInTheDocument();
    expect(screen.queryByText("Net worth")).not.toBeInTheDocument();
  });

  // AccountsPanel isn't mounted at all in this state (see FirstRunPanel
  // above it in the branch), so its own "+ Add account" button -- wired
  // first -- would never be reachable by exactly the household that needs
  // account creation most: one with nothing in it yet. This is the other
  // trigger for the same modal, and it has to open on its own.
  it("opens the add-account modal from the first-run panel's own button", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 0,
            assetsMinor: 0, liabilitiesMinor: 0,
            breakdown: [], excludedNoRate: [], excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    fireEvent.click(await screen.findByText(FINANCES_COPY.addAccount));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });

  it("shows net worth and one bar per populated type, gating + Add account to the owner", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "DBS Everyday", type: "cash",
            ownerMembershipId: null, ownerName: null,
            balance: { amountMinor: 824055, currency: "SGD" },
            balanceAsOf: "2026-07-26",
            countTowardNetWorth: true, visibleToLimitedMembers: false,
            archivedAt: null,
          }],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 824055,
            assetsMinor: 824055, liabilitiesMinor: 0,
            breakdown: [{ type: "cash", totalMinor: 824055 }],
            excludedNoRate: [], excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    // The single account IS the household's entire net worth here, so
    // "S$8,240.55" is the correct figure in three places at once: the net
    // worth headline, the cash row of the breakdown, and that row's own net
    // total -- plus the account's own balance in the accounts list. Each
    // card carries an accessible name (aria-labelledby) precisely so an
    // assertion can say *which* occurrence it means instead of asking
    // findByText to guess among four identical strings.
    const netWorthCard = await screen.findByRole("region", { name: "Net worth" });
    // Awaiting the card only waits for the accounts query; the figure's
    // symbol comes from the separate useCurrencies() query, so this needs its
    // own await -- see FinancesPage.test.tsx's fix-round-1 report entry.
    expect(await within(netWorthCard).findByText("S$8,240.55")).toBeInTheDocument();

    const breakdownCard = screen.getByRole("region", { name: "Assets & liabilities" });
    expect(within(breakdownCard).getByText("Cash & savings")).toBeInTheDocument();
    expect(within(breakdownCard).queryByText("Property")).not.toBeInTheDocument();

    const accountsPanel = screen.getByRole("region", { name: "Accounts" });
    expect(within(accountsPanel).getByText("DBS Everyday")).toBeInTheDocument();
    // The button's visibility comes from useMe()'s isOwner, a query separate
    // from the accounts fetch this test has otherwise awaited -- same reason
    // as the net worth figure above.
    expect(await within(accountsPanel).findByText(FINANCES_COPY.addAccount)).toBeInTheDocument();
  });

  // The state a household reaches by changing its primary currency in
  // Settings. A zero here would say they have nothing.
  it("shows no figure at all when nothing can be converted", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "Chase", type: "cash",
            ownerMembershipId: null, ownerName: null,
            balance: { amountMinor: 500000, currency: "USD" },
            balanceAsOf: "2026-07-26",
            countTowardNetWorth: true, visibleToLimitedMembers: false,
            archivedAt: null,
          }],
          summary: {
            currency: "SGD", computable: false,
            breakdown: [],
            excludedNoRate: [{ accountId: "a1", currency: "USD" }],
            excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText(/no exchange rate between SGD and USD/)).toBeInTheDocument();
    expect(screen.queryByText("S$0.00")).not.toBeInTheDocument();
  });

  // A limited member's response carries no summary and no amounts. The page
  // must not synthesise either, and their own "+ Add account" stays hidden --
  // Task 40's create form is owner-only, same as the button that opens it.
  it("shows a limited member the shared accounts and no figures", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("limited") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "OCBC Joint Savings", type: "cash",
            ownerMembershipId: null, ownerName: null,
            countTowardNetWorth: true, visibleToLimitedMembers: true,
            archivedAt: null,
          }],
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText("OCBC Joint Savings")).toBeInTheDocument();
    expect(screen.queryByText("Net worth")).not.toBeInTheDocument();
    expect(screen.queryByText(/S\$/)).not.toBeInTheDocument();
    expect(screen.queryByText(FINANCES_COPY.addAccount)).not.toBeInTheDocument();
  });
});
