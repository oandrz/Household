import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "../auth/schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { AccountModal } from "./AccountModal";

const ORIGINAL_TZ = process.env.TZ;

afterEach(() => {
  vi.unstubAllGlobals();
  // Only the timezone test below touches these; harmless no-ops otherwise.
  vi.useRealTimers();
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
});

// AccountModal reads the household's primary currency off useMe() (to
// default the Balance currency select for a fresh "add" form) and this
// modal's own tests mount it with a cold QueryClient, so -- like
// FinancesPage.test.tsx -- every scenario here registers GET /auth/me
// explicitly rather than relying on stubFetchRoutes's registered-or-throw
// default to happen to not matter.
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

// GET /household/members answers a bare JSON array (member_handlers.go's
// handleListMembers writes `out`, a []memberViewDTO, straight to the
// response -- there is no wrapping object), matching membersListSchema's
// z.array(memberSchema) on this side.
const NO_MEMBERS = { status: 200, body: [] };

describe("AccountModal", () => {
  it("posts what the form was given, in minor units", async () => {
    let posted: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
      // 201, matching what POST /accounts actually answers (account_handlers.go's
      // writeAccount): apiFetch treats every non-204 2xx identically (parses
      // the body, returns it), so this isn't load-bearing for the test to
      // pass, but stubbing the status this route actually sends is what a
      // fixture is for.
      "POST /api/v1/accounts": {
        status: 201,
        body: {
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          openingBalance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        },
        capture: (body) => { posted = body; },
      },
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    // renderWithRouter's RouterProvider does not mount its route synchronously
    // -- even a trivial root route goes through an async pending state, so
    // nothing in this tree exists yet on the very next line without an await.
    // findByLabelText both waits that out and gets the first field.
    fireEvent.change(await screen.findByLabelText("Nickname"), { target: { value: "DBS Everyday" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "8240.55" } });
    // The currency select defaults to the household's primary once useMe()
    // resolves -- a second, independent async gap this test has to wait out
    // (unlike the debt test below), because it is the one assertion here that
    // actually depends on the value that default produces. Firing the click
    // before this resolves would post whatever the select's initial, pre-load
    // value was instead.
    await waitFor(() => expect(screen.getByLabelText("Currency")).toHaveValue("SGD"));
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    await waitFor(() => expect(posted).toBeDefined());
    expect(posted).toMatchObject({
      nickname: "DBS Everyday",
      type: "cash",
      openingBalanceMinor: 824055,
      openingBalanceCurrency: "SGD",
    });
  });

  // The rule that stops a car loan from making a household look richer, said
  // in the form rather than only in a 422. The backend refuses it either way.
  it("tells you to enter a debt as a positive amount", async () => {
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    fireEvent.change(await screen.findByLabelText("Nickname"), { target: { value: "Car loan" } });
    fireEvent.change(screen.getByLabelText("Type"), { target: { value: "loan" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "-14500" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    expect(
      await screen.findByText(/Enter what you owe as a positive amount/),
    ).toBeInTheDocument();
    // The field error has to stop the request, not just accompany it -- if
    // this fired the POST anyway, an unregistered route would turn into a
    // mutation error whose message could coincidentally satisfy the assertion
    // above without the guard actually having done anything.
    expect(
      fetchMock.mock.calls.some(([input]) => String(input) === "/api/v1/accounts"),
    ).toBe(false);
  });

  it("tells you to enter a number when the balance can't be parsed", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    fireEvent.change(await screen.findByLabelText("Nickname"), { target: { value: "Car loan" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "eight" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    // Distinct copy from the debt message above: "not a number" and "that
    // number has the wrong sign" are different mistakes.
    expect(await screen.findByText("Enter an amount, like 8240.55.")).toBeInTheDocument();
  });

  // Pins the fix for the wrong copy on a currency switch mid-edit: an SGD
  // account showing "8240.55" fails toMinorUnits the moment Currency moves to
  // a no-decimal currency, even though Balance itself was never touched.
  // Restating the figure back ("Enter an amount, like 8240.55.") describes
  // exactly what's already in the field; the fix names the actual cause.
  it("names the currency, not the figure, when Currency switches to one with no cents", async () => {
    const currenciesWithIDR = {
      status: 200,
      body: {
        currencies: [
          { code: "SGD", symbol: "S$", name: "Singapore dollar" },
          { code: "IDR", symbol: "Rp", name: "Indonesian rupiah" },
        ],
      },
    };
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": currenciesWithIDR,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(
      <AccountModal
        open
        onClose={() => {}}
        account={{
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          openingBalance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        }}
      />,
    );

    expect(await screen.findByLabelText("Starting balance")).toHaveValue("8240.55");
    // The Currency <select>'s own options only exist once useCurrencies()
    // resolves -- setting .value to "IDR" before its <option> exists is a
    // silent no-op in jsdom (as in a real browser), which would leave
    // `currency` at "SGD" and defeat the whole scenario. Waiting for the
    // option itself is the same "the field's value is ready but its options
    // aren't yet" async gap the prefill test above waits out for Owner.
    await screen.findByRole("option", { name: "IDR" });
    fireEvent.change(screen.getByLabelText("Currency"), { target: { value: "IDR" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("IDR doesn't use cents. Remove the decimal point."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Enter an amount, like 8240.55.")).not.toBeInTheDocument();
  });

  it("prefills an existing account and PATCHes the change, clearing Owner to Shared", async () => {
    let patched: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": {
        status: 200,
        body: [{ id: "m2", user: { id: "u2", email: "", displayName: "Christine", avatarInitial: "C" }, role: "owner", capabilities: [] }],
      },
      "PATCH /api/v1/accounts/a1": {
        status: 200,
        body: {
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          openingBalance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        },
        capture: (body) => { patched = body; },
      },
    });

    renderWithRouter(
      <AccountModal
        open
        onClose={() => {}}
        account={{
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: "m2", ownerName: "Christine",
          balance: { amountMinor: 824055, currency: "SGD" },
          openingBalance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        }}
      />,
    );

    expect(await screen.findByLabelText("Nickname")).toHaveValue("DBS Everyday");
    expect(screen.getByLabelText("Starting balance")).toHaveValue("8240.55");
    // The Owner select's own <option>s only exist once the household members
    // list resolves -- the field's *value* (this component's own state) is
    // already "m2" on first render, but the option to display it against
    // arrives on its own, independent async gap.
    await waitFor(() => expect(screen.getByLabelText("Owner")).toHaveValue("m2"));
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Owner"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(patched).toBeDefined());
    // updateAccountRequest.OwnerMembershipID is nil-means-unchanged
    // (account_handlers.go): posting JSON null here would leave Christine as
    // the owner. Only "" actually clears it to Shared.
    expect(patched).toMatchObject({ ownerMembershipId: "" });
  });

  // Real-patch handling: an edit that never goes near Balance or Currency
  // must never resend either field. Without this, opening an account whose
  // stored minor units aren't an exact multiple of 100 -- unreachable from
  // this form, but not prevented anywhere else (a direct API call, a CSV
  // import, a future bank-sync adapter) -- to change only its nickname would
  // prefill Balance from a truncated display value and PATCH that back,
  // silently moving the stored figure by up to 99 minor units.
  it("does not resend the balance when an edit never touches it", async () => {
    let patched: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
      "PATCH /api/v1/accounts/a1": {
        status: 200,
        body: {
          id: "a1", nickname: "DBS Everyday (renamed)", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          openingBalance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        },
        capture: (body) => { patched = body; },
      },
    });

    renderWithRouter(
      <AccountModal
        open
        onClose={() => {}}
        account={{
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          openingBalance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        }}
      />,
    );

    fireEvent.change(await screen.findByLabelText("Nickname"), {
      target: { value: "DBS Everyday (renamed)" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(patched).toBeDefined());
    expect(patched).not.toHaveProperty("openingBalanceMinor");
    expect(patched).not.toHaveProperty("openingBalanceCurrency");
    expect(patched).toMatchObject({ nickname: "DBS Everyday (renamed)" });
  });

  // --- opening balance vs current balance ---------------------------------
  //
  // These two accounts have a `balance` that differs from their
  // `openingBalance`, which is the ordinary state of every account once it
  // has a transaction on it. Every other fixture in this file gives the two
  // the same figure (an account with no transactions), so only these two can
  // tell the fields apart at all.
  const WITH_TRANSACTIONS_SINCE = {
    id: "a1", nickname: "DBS Everyday", type: "cash" as const,
    ownerMembershipId: null, ownerName: null,
    // S$1,000 asserted true on 1 June, S$300 of transactions since.
    balance: { amountMinor: 130000, currency: "SGD" },
    openingBalance: { amountMinor: 100000, currency: "SGD" },
    balanceAsOf: "2026-06-01",
    countTowardNetWorth: true, visibleToLimitedMembers: false,
    archivedAt: null,
  };

  // The field writes back to opening_balance_minor, so it must show the
  // opening balance. Prefilling it from the current balance and saving would
  // restate S$1,300 as the figure that was true on 1 June -- the household's
  // net worth then jumps by the S$300 of transactions that are still counted
  // on top of it, and every later edit compounds that.
  it("prefills Starting balance from the opening balance, not the current one", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(
      <AccountModal open onClose={() => {}} account={WITH_TRANSACTIONS_SINCE} />,
    );

    expect(await screen.findByLabelText("Starting balance")).toHaveValue("1000.00");
  });

  // Changing only Currency expresses no intent about the balance, but it
  // still has to resend one -- an amount and its currency are a single fact,
  // and sending the new code alone would reinterpret the stored minor units.
  // So the guarantee here is not "sends nothing" but "sends the same figure":
  // the opening balance, unchanged, in the newly chosen currency.
  it("does not change the opening balance when an edit only changes the currency", async () => {
    let patched: Record<string, unknown> | undefined;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": {
        status: 200,
        body: {
          currencies: [
            { code: "SGD", symbol: "S$", name: "Singapore dollar" },
            { code: "USD", symbol: "US$", name: "US dollar" },
          ],
        },
      },
      "GET /api/v1/household/members": NO_MEMBERS,
      "PATCH /api/v1/accounts/a1": {
        status: 200,
        body: { ...WITH_TRANSACTIONS_SINCE, openingBalance: { amountMinor: 100000, currency: "USD" } },
        capture: (body) => { patched = body as Record<string, unknown>; },
      },
    });

    renderWithRouter(
      <AccountModal open onClose={() => {}} account={WITH_TRANSACTIONS_SINCE} />,
    );

    // Same async gap as the IDR test above: the <option> has to exist before
    // setting .value to it does anything at all.
    await screen.findByRole("option", { name: "USD" });
    fireEvent.change(screen.getByLabelText("Currency"), { target: { value: "USD" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(patched).toBeDefined());
    expect(patched).toMatchObject({ openingBalanceMinor: 100000, openingBalanceCurrency: "USD" });
  });

  // today() (AccountModal.tsx) reads the *local* calendar date rather than
  // converting through UTC -- this branch has already produced three bugs of
  // exactly that class (f61407d "stop refusing today's date east of UTC",
  // f17be2d "fix dateOnly, the third instance of one mistake", and the plan
  // correction behind both), so this is the fourth site with the same hazard
  // and the only one that had no test. `toFake: ["Date"]` freezes only what
  // `new Date()` returns, leaving setTimeout alone -- the router's own
  // pending-state transition and findByLabelText's polling both still need
  // real timers to ever resolve.
  it("defaults Balance as of to the local calendar day, not the UTC one", async () => {
    process.env.TZ = "Pacific/Midway"; // UTC-11
    vi.useFakeTimers({ toFake: ["Date"] });
    // 05:00 UTC on 1 Jan is still 18:00 on 31 Dec in Pacific/Midway.
    vi.setSystemTime(new Date("2026-01-01T05:00:00Z"));

    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/household/members": NO_MEMBERS,
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    expect(await screen.findByLabelText("Starting balance as of")).toHaveValue("2025-12-31");
  });
});
