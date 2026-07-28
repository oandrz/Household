import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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

  // Pins the fix for "archiving the last account makes it permanently
  // unrestorable": before this, FirstRunPanel had no "Show archived" toggle
  // at all, so a household reduced to zero live accounts had no way back to
  // the one it had just archived.
  //
  // This does NOT pin invalidateAccounts's `return` (useAccounts.ts) -- a
  // reviewer showed that dropping it leaves this test (and the other six in
  // this file) green, because `queryClient.invalidateQueries` dispatches its
  // refetch regardless of whether the caller awaits the returned promise,
  // and `findByText` below polls the DOM rather than mutation state, so it
  // cannot tell "settled because the refetch landed" apart from "settled
  // because nobody waited." See the dedicated test below this one for what
  // the `return` actually gates.
  it("keeps the archived toggle reachable after archiving a household's only account", async () => {
    const oneAccount = {
      id: "a1", nickname: "DBS Everyday", type: "cash",
      ownerMembershipId: null, ownerName: null,
      balance: { amountMinor: 824055, currency: "SGD" },
      balanceAsOf: "2026-07-26",
      countTowardNetWorth: true, visibleToLimitedMembers: false,
      archivedAt: null,
    };
    const zeroSummary = {
      currency: "SGD", computable: true, netWorthMinor: 0,
      assetsMinor: 0, liabilitiesMinor: 0,
      breakdown: [], excludedNoRate: [], excludedByChoice: 0,
    };

    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": [
        {
          status: 200,
          body: {
            accounts: [oneAccount],
            summary: {
              currency: "SGD", computable: true, netWorthMinor: 824055,
              assetsMinor: 824055, liabilitiesMinor: 0,
              breakdown: [{ type: "cash", totalMinor: 824055 }],
              excludedNoRate: [], excludedByChoice: 0,
            },
          },
        },
        { status: 200, body: { accounts: [], summary: zeroSummary } },
      ],
      "POST /api/v1/accounts/a1/archive": {
        status: 200,
        body: { ...oneAccount, archivedAt: "2026-07-28T00:00:00Z" },
      },
    });

    renderWithRouter(<FinancesPage />);

    fireEvent.click(await screen.findByRole("button", { name: "Archive DBS Everyday" }));

    expect(await screen.findByText(FINANCES_COPY.emptyTitle)).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: FINANCES_COPY.archivedToggle })).toBeInTheDocument();
  });

  // Pins invalidateAccounts's `return` (useAccounts.ts, deferred finding 7)
  // directly, rather than through the DOM eventually catching up. What the
  // `return` actually controls is when useSetAccountArchived's mutation is
  // considered "settled": TanStack Query awaits a returned onSuccess promise
  // before running a mutate()-level onSettled, and AccountsPanel's own
  // per-row `pendingIds` (the Archive/Restore button's `disabled`) is cleared
  // from exactly that onSettled. So the button must stay disabled for as
  // long as the post-archive refetch is still in flight, and only that.
  //
  // stubFetchRoutes can't hold a response open, so this uses a hand-rolled
  // fetch mock instead -- the same deferred-promise shape
  // SignInScreen.test.tsx uses for its own in-flight assertion -- to keep the
  // refetch GET unresolved until the test says so.
  it("keeps the archive button disabled until the post-archive refetch settles", async () => {
    const oneAccount = {
      id: "a1", nickname: "DBS Everyday", type: "cash",
      ownerMembershipId: null, ownerName: null,
      balance: { amountMinor: 824055, currency: "SGD" },
      balanceAsOf: "2026-07-26",
      countTowardNetWorth: true, visibleToLimitedMembers: false,
      archivedAt: null,
    };
    const zeroSummary = {
      currency: "SGD", computable: true, netWorthMinor: 0,
      assetsMinor: 0, liabilitiesMinor: 0,
      breakdown: [], excludedNoRate: [], excludedByChoice: 0,
    };

    let accountsGetCount = 0;
    let resolveRefetch!: () => void;

    const jsonResponse = (status: number, body: unknown) =>
      Promise.resolve(
        new Response(JSON.stringify(body), {
          status,
          headers: { "Content-Type": "application/json" },
        }),
      );

    const fetchMock = vi.fn<
      (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
    >((input, init) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const url = String(input);

      if (method === "GET" && url === "/api/v1/auth/me") {
        return jsonResponse(200, meFixture("owner"));
      }
      if (method === "GET" && url === "/api/v1/currencies") {
        return jsonResponse(200, CURRENCIES);
      }
      if (method === "GET" && url === "/api/v1/accounts") {
        accountsGetCount += 1;
        if (accountsGetCount === 1) {
          return jsonResponse(200, {
            accounts: [oneAccount],
            summary: {
              currency: "SGD", computable: true, netWorthMinor: 824055,
              assetsMinor: 824055, liabilitiesMinor: 0,
              breakdown: [{ type: "cash", totalMinor: 824055 }],
              excludedNoRate: [], excludedByChoice: 0,
            },
          });
        }
        // The refetch invalidateAccounts's onSuccess kicks off. Held open
        // until resolveRefetch() is called below, so the window between the
        // archive POST settling and this GET settling is observable instead
        // of collapsing to a single microtask batch.
        return new Promise<Response>((resolve) => {
          resolveRefetch = () =>
            resolve(
              new Response(JSON.stringify({ accounts: [], summary: zeroSummary }), {
                status: 200,
                headers: { "Content-Type": "application/json" },
              }),
            );
        });
      }
      // include_archived=true is invalidated alongside the false-keyed query
      // above, but it is never mounted here (FinancesPage defaults
      // includeArchived to false and this test never touches the toggle), so
      // invalidateQueries's default refetchType: "active" should mark it
      // stale without refetching. Answered anyway, immediately, so a future
      // TanStack Query version that refetches inactive queries too fails
      // this test with a clear "unexpected fetch" message rather than a
      // silent hang if this branch were absent.
      if (method === "GET" && url === "/api/v1/accounts?include_archived=true") {
        return jsonResponse(200, { accounts: [], summary: zeroSummary });
      }
      if (method === "POST" && url === "/api/v1/accounts/a1/archive") {
        return jsonResponse(200, { ...oneAccount, archivedAt: "2026-07-28T00:00:00Z" });
      }
      throw new Error(`unexpected fetch call: ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithRouter(<FinancesPage />);

    const archiveButton = await screen.findByRole("button", { name: "Archive DBS Everyday" });
    fireEvent.click(archiveButton);

    // Marks the moment the archive POST has resolved and invalidateAccounts's
    // onSuccess has run far enough to dispatch the refetch -- the checkpoint
    // this whole test is built around. Everything asserted right after this
    // line is asserted at that exact moment, before the held-open GET above
    // has had any chance to settle.
    await waitFor(() => expect(accountsGetCount).toBe(2));
    expect(archiveButton).toBeDisabled();

    resolveRefetch();

    expect(await screen.findByText(FINANCES_COPY.emptyTitle)).toBeInTheDocument();
  });

  // The other half of the same fix: FirstRunPanel must stay put when its own
  // toggle is switched on and there is still nothing to show, rather than
  // falling through to the three cards below with no accounts behind them.
  // This is the state a household that has never held an account reaches the
  // instant it tries the toggle -- distinct from the test above, where the
  // toggle is never touched -- and it only exists because `rows` is
  // parameterised by `includeArchived` (useAccounts.ts): switching the
  // toggle refetches GET /api/v1/accounts?include_archived=true, which for a
  // true first-run household still answers with zero rows.
  it("stays on the first-run panel when its own toggle finds nothing archived either", async () => {
    const zeroSummary = {
      currency: "SGD", computable: true, netWorthMinor: 0,
      assetsMinor: 0, liabilitiesMinor: 0,
      breakdown: [], excludedNoRate: [], excludedByChoice: 0,
    };

    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: zeroSummary } },
      "GET /api/v1/accounts?include_archived=true": {
        status: 200,
        body: { accounts: [], summary: zeroSummary },
      },
    });

    renderWithRouter(<FinancesPage />);

    fireEvent.click(
      await screen.findByRole("switch", { name: FINANCES_COPY.archivedToggle }),
    );

    expect(await screen.findByText(FINANCES_COPY.emptyTitle)).toBeInTheDocument();
    expect(screen.queryByText("Net worth")).not.toBeInTheDocument();
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
