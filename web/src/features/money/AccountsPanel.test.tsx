// Task 40's own behaviours for this panel: an owner's edit/archive/restore
// affordances and the shared invalidation behind them. The read-only render
// (balance redaction, the archived toggle, the empty states) is already
// covered through FinancesPage.test.tsx; this file only adds what Task 40
// introduced.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "../auth/schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AccountsPanel } from "./AccountsPanel";
import type { Account } from "./schemas";

afterEach(() => vi.unstubAllGlobals());

function meFixture(): Me {
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
      role: "owner",
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
  };
}

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

// balance and openingBalance differ, as they do on any account with a
// transaction on it: S$8,000 asserted true on 26 July, S$240.55 of
// transactions since. A fixture giving them the same figure cannot tell the
// row's display (which wants the current one) from the edit form's prefill
// (which wants the opening one), and both are asserted below.
const LIVE_ACCOUNT: Account = {
  id: "a1", nickname: "DBS Everyday", type: "cash",
  ownerMembershipId: null, ownerName: null,
  balance: { amountMinor: 824055, currency: "SGD" },
  openingBalance: { amountMinor: 800000, currency: "SGD" },
  balanceAsOf: "2026-07-26",
  countTowardNetWorth: true, visibleToLimitedMembers: false,
  archivedAt: null,
};

const ARCHIVED_ACCOUNT: Account = {
  ...LIVE_ACCOUNT,
  archivedAt: "2026-07-20T00:00:00Z",
};

function renderPanel(accounts: Account[], includeArchived = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AccountsPanel
        accounts={accounts}
        includeArchived={includeArchived}
        onIncludeArchivedChange={() => {}}
      />
    </QueryClientProvider>,
  );
}

describe("AccountsPanel", () => {
  it("opens the edit modal populated with the row's own values", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": { status: 200, body: [] },
    });

    renderPanel([LIVE_ACCOUNT]);

    fireEvent.click(await screen.findByRole("button", { name: "Edit DBS Everyday" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Nickname")).toHaveValue("DBS Everyday");
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeInTheDocument();
    // The row shows the current balance and the form asks for the opening
    // one. Asserted here, at the level where a click actually opens the
    // modal against a row's own Account object, because this is the seam the
    // defect lived in: the modal was correct about every field it was given
    // and wrong about which figure it was given for this one.
    expect(within(dialog).getByLabelText("Starting balance")).toHaveValue("8000.00");
  });

  // AccountsPanel takes its rows as a prop rather than fetching them itself
  // (FinancesPage owns the useAccounts() call above it), so this test proves
  // the click reaches POST .../archive with no window.confirm in between --
  // not that the list refreshes, which needs a query in the cache to
  // invalidate and belongs at the FinancesPage level that actually mounts one.
  it("archives a live account without asking through window.confirm", async () => {
    const confirmSpy = vi.spyOn(window, "confirm");
    let archiveCalled = false;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "POST /api/v1/accounts/a1/archive": {
        status: 200,
        body: { ...LIVE_ACCOUNT, archivedAt: "2026-07-28T00:00:00Z" },
        capture: () => { archiveCalled = true; },
      },
    });

    renderPanel([LIVE_ACCOUNT]);

    fireEvent.click(await screen.findByRole("button", { name: "Archive DBS Everyday" }));

    await waitFor(() => expect(archiveCalled).toBe(true));
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it("shows a restore action for an archived account and posts to /restore", async () => {
    let restoreCalled = false;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "POST /api/v1/accounts/a1/restore": {
        status: 200,
        body: { ...ARCHIVED_ACCOUNT, archivedAt: null },
        capture: () => { restoreCalled = true; },
      },
    });

    renderPanel([ARCHIVED_ACCOUNT], true);

    const restoreButton = await screen.findByRole("button", { name: "Restore DBS Everyday" });
    fireEvent.click(restoreButton);

    await waitFor(() => expect(restoreCalled).toBe(true));
  });
});
