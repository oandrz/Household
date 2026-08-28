// Follows FinancesPage.test.tsx's stub-and-provider setup: renderWithRouter
// (this page carries a real `<Link to="/money">`, which throws outside a
// router context) plus stubFetchRoutes for every request.
//
// fireEvent, not the brief's own "@testing-library/user-event" sketch --
// that package isn't one of this project's dependencies (only
// @testing-library/react and jest-dom are; TransactionModal.test.tsx hit the
// same gap and settled on fireEvent for the same reason).
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { TransactionsPage } from "./TransactionsPage";
import type { Account } from "./schemas";
import type { MonthSummary, Transaction } from "./transactionSchemas";

const CURRENCIES = {
  status: 200,
  body: {
    currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }],
  },
};

// The one account every fixture below attaches its transactions to.
// "DBS Everyday" specifically, because the pre-opening-balance test asserts
// the account's own *name* appears in the marker, not merely that some
// marker rendered.
function accountFixture(overrides: Partial<Account> = {}): Account {
  return {
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
    ...overrides,
  };
}

// id "txn-1" and description "Cold Storage" are load-bearing: the edit and
// delete tests assert PATCH/DELETE against "/api/v1/transactions/txn-1", and
// the row-click tests find the row by its "Cold Storage" accessible name.
function expenseFixture(overrides: Partial<Transaction> = {}): Transaction {
  return {
    id: "txn-1",
    kind: "expense",
    occurredOn: "2026-07-18",
    description: "Cold Storage",
    categoryId: null,
    categoryName: null,
    paidByMembershipId: null,
    paidByName: null,
    fromAccountId: "a1",
    fromAccountName: "DBS Everyday",
    toAccountId: null,
    toAccountName: null,
    amount: { amountMinor: 5230, currency: "SGD" },
    receivedAmount: null,
    beforeFromAccountOpeningBalance: false,
    beforeToAccountOpeningBalance: null,
    ...overrides,
  };
}

// A same-currency transfer carrying a real bank fee as its receivedAmount --
// the fixture the clearReceivedAmount test below edits into an expense.
function transferFixture(overrides: Partial<Transaction> = {}): Transaction {
  return {
    id: "txn-1",
    kind: "transfer",
    occurredOn: "2026-07-18",
    description: "Transfer to OCBC",
    categoryId: null,
    categoryName: null,
    paidByMembershipId: null,
    paidByName: null,
    fromAccountId: "a1",
    fromAccountName: "DBS Everyday",
    toAccountId: "a2",
    toAccountName: "OCBC 360",
    amount: { amountMinor: 5000, currency: "SGD" },
    receivedAmount: { amountMinor: 4980, currency: "SGD" },
    beforeFromAccountOpeningBalance: false,
    beforeToAccountOpeningBalance: false,
    ...overrides,
  };
}

type SummaryInput = Partial<MonthSummary> & {
  count: number;
  spentMinor: number;
};

function fullSummary(input: SummaryInput): MonthSummary {
  return {
    currency: "SGD",
    month: "2026-07",
    excludedNoRate: [],
    ...input,
  };
}

// renderPage builds every route TransactionsPage fires on mount (currencies,
// categories, household members, accounts, the unfiltered ledger, and a
// default POST route for Add) plus whatever a given test additionally needs
// -- a *second*, differently-keyed ledger route for the filtered case, or the
// PATCH/DELETE routes for "txn-1". Registered every time (not just when a
// test exercises them): stubFetchRoutes only complains about a route that is
// *called* and unregistered, so an unused stub here is harmless.
//
// extraRoutes merges in routes a specific test needs beyond the defaults
// above (a second ledger page, a PATCH target for a transaction other than
// "txn-1") -- stubFetchRoutes can only be called once per test (a second call
// replaces the global fetch stub entirely, discarding every route the first
// call registered), so every route a test needs has to go through this one
// call.
function renderPage(options: {
  transactions: Transaction[];
  summary: SummaryInput;
  filtered?: { transactions: Transaction[]; summary: SummaryInput };
  allMonths?: { transactions: Transaction[]; summary: SummaryInput };
  nextCursor?: string | null;
  accounts?: Account[];
  extraRoutes?: Record<string, RouteResponse | RouteResponse[]>;
}) {
  const patched = vi.fn();
  const deleted = vi.fn();
  const posted = vi.fn();

  const routes: Record<string, RouteResponse | RouteResponse[]> = {
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/categories": { status: 200, body: { categories: [] } },
    "GET /api/v1/household/members": { status: 200, body: [] },
    "GET /api/v1/accounts": {
      status: 200,
      body: { accounts: options.accounts ?? [accountFixture()] },
    },
    // No month in the key: the page opens with its month filter unchosen, so
    // the first request carries none and the server decides which month that
    // means (parseTransactionFilter). The Month control still shows that
    // month -- it is resolved for display from the response, not written into
    // the filter state -- so no second request follows.
    "GET /api/v1/transactions": {
      status: 200,
      body: {
        transactions: options.transactions,
        nextCursor: options.nextCursor ?? null,
        summary: fullSummary(options.summary),
      },
    },
    "POST /api/v1/transactions": {
      status: 201,
      body: expenseFixture({ id: "txn-new" }),
      capture: (body) => posted("/api/v1/transactions", body),
    },
    "PATCH /api/v1/transactions/txn-1": {
      status: 200,
      body: expenseFixture({ description: "Cold Storage — milk" }),
      capture: (body) => patched("/api/v1/transactions/txn-1", body),
    },
    // body: undefined, not null -- a 204 is a null-body status by the Fetch
    // spec, and stubFetchRoutes always constructs `new Response(...)` from
    // whatever's here. `JSON.stringify(null)` is the string "null", a
    // non-null body the real Response constructor refuses to pair with a 204
    // (throws synchronously); `JSON.stringify(undefined)` is `undefined`,
    // which is a legal null body. Caught by extending this test past the
    // `deleted` spy (see the assertion below) -- the spy alone stayed green
    // even while this exact mistake made the underlying request throw and
    // the modal never close.
    "DELETE /api/v1/transactions/txn-1": {
      status: 204,
      body: undefined,
      capture: () => deleted("/api/v1/transactions/txn-1"),
    },
    ...options.extraRoutes,
  };

  if (options.filtered) {
    // Carries the month as well as the kind: changing any filter commits the
    // month the control was already showing, so from then on the request
    // names it explicitly. Same month, same rows -- but a different key, and
    // a stub that ignored the querystring would hide that.
    routes[
      `GET /api/v1/transactions?kind=income&month=${fullSummary(options.summary).month}`
    ] = {
      status: 200,
      body: {
        transactions: options.filtered.transactions,
        nextCursor: null,
        summary: fullSummary(options.filtered.summary),
      },
    };
  }

  if (options.allMonths) {
    // month=all, not an absent month: an absent one is what the page sends
    // before anything is chosen, and the server reads that as the current
    // month. The widened request has to be its own key or this stub would let
    // "widen the ledger" quietly resolve to "the same month again".
    routes["GET /api/v1/transactions?month=all"] = {
      status: 200,
      body: {
        transactions: options.allMonths.transactions,
        nextCursor: null,
        summary: fullSummary(options.allMonths.summary),
      },
    };
  }

  stubFetchRoutes(routes);

  return {
    ...renderWithRouter(<TransactionsPage />),
    patched,
    deleted,
    posted,
  };
}

describe("TransactionsPage", () => {
  // The Month control used to open blank, so Chrome drew its own empty-month
  // placeholder -- a row of dashes that reads as a broken control rather than
  // as "any month". It now shows the month the server says the figures above
  // the ledger describe, which is also the month the ledger itself is scoped
  // to (parseTransactionFilter defaults both halves together).
  it("opens on the month the summary describes, not blank", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    expect(await screen.findByLabelText(/month/i)).toHaveValue("2026-07");
  });

  // "Spent this month" is a claim, and it is only true while the month is the
  // server's own default. Choosing July left that label sitting above a July
  // figure under a header already reading "10 in July 2026" -- found in the
  // browser walk, one row below the defect this change started from.
  it("names the month on the spend figure once the household picks one", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      extraRoutes: {
        "GET /api/v1/transactions?month=2026-06": {
          status: 200,
          body: {
            transactions: [],
            nextCursor: null,
            summary: fullSummary({ month: "2026-06", count: 0, spentMinor: 0 }),
          },
        },
      },
    });

    expect(await screen.findByText(/spent this month/i)).toBeInTheDocument();

    fireEvent.change(await screen.findByLabelText(/month/i), {
      target: { value: "2026-06" },
    });

    expect(await screen.findByText(/spent in June 2026/i)).toBeInTheDocument();
    expect(screen.queryByText(/spent this month/i)).toBeNull();
  });

  // The ledger opens on the current month, so an empty response no longer
  // means an empty ledger -- it means an empty month, and the screen must not
  // claim more than it knows. The first-run panel belongs to the widened
  // state, the only one that can honestly say nothing has ever been logged.
  it("names the empty month rather than claiming the ledger is empty", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      allMonths: { transactions: [], summary: { count: 0, spentMinor: 0 } },
    });

    expect(
      await screen.findByText(/nothing logged in july 2026/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/nothing logged yet/i)).toBeNull();
  });

  it("shows the first-run panel once every month is shown and there is still nothing", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      allMonths: { transactions: [], summary: { count: 0, spentMinor: 0 } },
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /show every month/i }),
    );

    expect(await screen.findByText(/nothing logged yet/i)).toBeInTheDocument();
  });

  // Widening drops the count rather than reprinting the month's own. The
  // summary still describes one calendar month (MonthSummary answers for
  // exactly one by construction), so "1 in July 2026" over an all-time list
  // would be the very mismatch the default's fix removed -- and the spend
  // figure keeps naming its month for the same reason.
  it("stops claiming a month over the list once every month is shown", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      allMonths: {
        transactions: [expenseFixture()],
        summary: { count: 1, spentMinor: 5230 },
      },
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /show every month/i }),
    );

    // The row is asserted too, not only the copy: the heading above is driven
    // by local state, so a widening that never reached the server as month=all
    // would still relabel the header while the ledger below it stayed scoped.
    expect(await screen.findByText("Cold Storage")).toBeInTheDocument();
    expect(screen.getByText(/every month/i)).toBeInTheDocument();
    expect(screen.queryByText(/1 in July 2026/i)).toBeNull();
    expect(screen.getByText(/spent in July 2026/i)).toBeInTheDocument();
  });

  // A household that filtered to "Income" and saw the first-run panel would
  // think its ledger had been wiped. The unfiltered fixture carries a real
  // row (so this genuinely starts non-empty) and the filtered response is
  // registered under its own, differently-keyed route -- a stub that ignored
  // the querystring would make this pass for the wrong reason.
  it("distinguishes an empty ledger from filters that match nothing", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      filtered: { transactions: [], summary: { count: 0, spentMinor: 0 } },
    });

    // Kind is a labelled radio group (the design's own segmented control),
    // not a single-value <select> -- clicking the "Income" option directly,
    // rather than changing one element's value, is what a real keyboard/
    // screen-reader user does with a native radio group.
    fireEvent.click(await screen.findByRole("radio", { name: "Income" }));

    expect(
      await screen.findByText(/nothing matches those filters/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/nothing logged yet/i)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /clear filters/i }),
    ).toBeInTheDocument();
  });

  // The Kind radio's real, keyboard-operable <input> is `sr-only` -- visually
  // hidden -- so the pill drawn by its <label> is the only thing a sighted
  // user sees. The Task 19 browser walk found that Tabbing or arrow-keying
  // through this group moved focus with no visible sign of it at all: jsdom's
  // fireEvent.click above cannot exercise real keyboard focus, so nothing
  // caught the label never reacting to the input's :focus-visible state. This
  // pins the fix's className -- it does not, and cannot in jsdom, prove the
  // ring actually renders on screen; that half was proved by the browser
  // walk itself (see docs/superpowers/plans/
  // 2026-07-29-hearth-transactions-verification.md, carried item C).
  //
  // Two colours, not one: the walk's first fix used a single dark-green ring
  // for every pill, and two screenshots of the selected-and-focused "Expense"
  // pill came back pixel-identical before and after that fix -- a dark ring
  // inset against the selected pill's near-black background is invisible.
  // The selected ("All", the default kind) and unselected ("Income") pills
  // now carry different ring colours for exactly that reason, so this test
  // checks both rather than either alone.
  it("gives the Kind radio group's label a focus-visible ring class, in a colour that reads against its own background", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    const selectedLabel = (
      await screen.findByRole("radio", { name: "All" })
    ).closest("label");
    const unselectedLabel = screen
      .getByRole("radio", { name: "Income" })
      .closest("label");

    expect(selectedLabel?.className).toMatch(
      /has-\[:focus-visible\]:ring-white/,
    );
    expect(unselectedLabel?.className).toMatch(
      /has-\[:focus-visible\]:ring-accent/,
    );
  });

  it("hides Load older transactions when there is no next cursor", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      nextCursor: null,
    });
    await screen.findByText("Cold Storage");
    expect(
      screen.queryByRole("button", { name: /load older/i }),
    ).not.toBeInTheDocument();
  });

  it("shows Load older transactions when the server sent a cursor", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      nextCursor: "2026-07-16:txn-9",
    });
    expect(
      await screen.findByRole("button", { name: /load older/i }),
    ).toBeInTheDocument();
  });

  // A quietly short total looks identical to a correct one.
  it("names the transactions left out of the month's spend", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: {
        count: 1,
        spentMinor: 0,
        excludedNoRate: [{ transactionId: "txn-1", currency: "USD" }],
      },
    });
    expect(
      await screen.findByText(/no exchange rate for USD/i),
    ).toBeInTheDocument();
  });

  // Naming the account matters: a transfer can predate one side's opening
  // balance and not the other's.
  it("marks a row that predates its account's opening balance, naming the account", async () => {
    renderPage({
      transactions: [
        { ...expenseFixture(), beforeFromAccountOpeningBalance: true },
      ],
      summary: { count: 1, spentMinor: 5230 },
    });
    expect(
      await screen.findByText(/before DBS Everyday's opening balance/i),
    ).toBeInTheDocument();
  });

  it("disables Add transaction when the household has no accounts", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      accounts: [],
    });
    expect(
      await screen.findByRole("button", { name: /add transaction/i }),
    ).toBeDisabled();
    expect(
      screen.getByText(/add an account first, and transactions can attach to it/i),
    ).toBeInTheDocument();
  });

  it("sends a household with no accounts to Finances from the empty state", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      accounts: [],
    });

    const link = await screen.findByRole("link", { name: /add an account/i });
    expect(link).toHaveAttribute("href", "/money");
    // The generic first-run copy would be a lie here: there is nothing to add
    // an expense *to* yet.
    expect(screen.queryByText(/nothing logged yet/i)).toBeNull();
  });

  it("keeps the generic first-run copy once an account exists", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      allMonths: { transactions: [], summary: { count: 0, spentMinor: 0 } },
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /show every month/i }),
    );

    expect(await screen.findByText(/nothing logged yet/i)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /add an account/i })).toBeNull();
  });

  // Editing is how a mistyped row gets corrected instead of deleted and
  // retyped, and it is the only caller PATCH /transactions/{id} has. Without
  // this the endpoint and its hook exist with nothing reaching them.
  it("opens the modal populated when a row is clicked, and patches on save", async () => {
    const { patched } = renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /Cold Storage/i }),
    );

    // Populated, not blank: an edit form that opens empty silently clears
    // every field the person does not retype.
    expect(screen.getByLabelText(/description/i)).toHaveValue("Cold Storage");
    expect(screen.getByLabelText(/^amount/i)).toHaveValue("52.30");

    fireEvent.change(screen.getByLabelText(/description/i), {
      target: { value: "Cold Storage — milk" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    await waitFor(() =>
      expect(patched).toHaveBeenCalledWith(
        "/api/v1/transactions/txn-1",
        // categoryId: "" (not null, and not absent) pins the null->""
        // translation toUpdateBody exists for -- expenseFixture's own
        // categoryId is null, and the PATCH route reads a *missing* or
        // *null* categoryId as "leave alone" (a pointer field server-side),
        // so sending the modal's null back verbatim would never actually
        // clear a category. clearReceivedAmount: false pins that the flag is
        // genuinely computed (this transaction never had a received amount
        // to clear), not merely absent from the body.
        expect.objectContaining({
          description: "Cold Storage — milk",
          categoryId: "",
          clearReceivedAmount: false,
        }),
      ),
    );
  });

  // The case toUpdateBody's clearReceivedAmount derivation exists for: a
  // transfer with a real, stored bank fee, edited into a different kind.
  // TransactionModal's own expense/income submit branch always sets
  // receivedAmountMinor: null -- indistinguishable, on its own, from a
  // non-transfer that never had a figure to begin with. Sending that null
  // straight through would decode server-side as "leave alone" (the PATCH
  // route's fields are pointers), silently stranding the old 49.80 in the
  // database attached to what the UI now shows as a plain expense.
  it("clears a stored received amount when editing turns a transfer into a non-transfer", async () => {
    const { patched } = renderPage({
      transactions: [transferFixture()],
      summary: { count: 1, spentMinor: 0 },
      accounts: [
        accountFixture(),
        accountFixture({ id: "a2", nickname: "OCBC 360" }),
      ],
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /Transfer to OCBC/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Expense" }));
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    await waitFor(() =>
      expect(patched).toHaveBeenCalledWith(
        "/api/v1/transactions/txn-1",
        expect.objectContaining({ clearReceivedAmount: true }),
      ),
    );
    const body = patched.mock.calls[0][1];
    expect(body).not.toHaveProperty("receivedAmountMinor");
  });

  it("removes a transaction and asks for confirmation first", async () => {
    const { deleted } = renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /Cold Storage/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: /delete/i }));
    // In-page confirmation, never window.confirm: a native dialog blocks every
    // browser event and would freeze an automated walk.
    fireEvent.click(screen.getByRole("button", { name: /yes, delete/i }));

    await waitFor(() =>
      expect(deleted).toHaveBeenCalledWith("/api/v1/transactions/txn-1"),
    );
    // Proves the deletion actually completed, not merely that the request
    // left: `capture` fires before stubFetchRoutes constructs its Response,
    // so a request that throws *after* being captured (as a wrongly-shaped
    // 204 stub does -- see the route's own comment above) would still
    // satisfy the assertion above while TransactionModal's onSubmit .catch
    // leaves the dialog open on a submitError. The dialog closing only
    // happens from onClose, which only runs once onDelete's promise actually
    // resolves.
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  // Review round, finding 2: nothing in this suite exercised the create path
  // at all before this test -- no POST route was registered and no test
  // clicked Add, so handleCreate was provably untouched by the suite even
  // though the tracker already called Add transaction built and verified.
  it("opens the modal blank when Add is clicked, and posts on save", async () => {
    const { posted } = renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    fireEvent.click(
      await screen.findByRole("button", { name: /add transaction/i }),
    );

    // Blank, not populated -- the opposite of the edit test's own assertion:
    // opening Add must never carry another row's values into a new one.
    expect(screen.getByLabelText(/description/i)).toHaveValue("");
    expect(screen.getByLabelText(/^amount/i)).toHaveValue("");

    fireEvent.change(screen.getByLabelText(/description/i), {
      target: { value: "Kopitiam lunch" },
    });
    fireEvent.change(screen.getByLabelText(/^amount/i), {
      target: { value: "8.40" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    await waitFor(() =>
      expect(posted).toHaveBeenCalledWith(
        "/api/v1/transactions",
        expect.objectContaining({
          description: "Kopitiam lunch",
          amountMinor: 840,
        }),
      ),
    );
  });

  // Review round, finding 1: olderRows (the rows "Load older transactions"
  // appends) sits outside the query cache invalidateLedger refreshes, so a
  // transaction edited while showing on an appended page used to keep
  // displaying its pre-edit description in place until a full reload -- the
  // row a household just corrected still showing the old figure, with no
  // staleness indicator at all. Loads a second page, edits a row on it, and
  // confirms the ledger shows the new description without any further
  // reload.
  it("shows an edited row's new value even when it sits on an already-loaded older page", async () => {
    const olderPage = {
      transactions: [
        expenseFixture({
          id: "txn-2",
          description: "Grab",
          occurredOn: "2026-07-17",
          amount: { amountMinor: 1480, currency: "SGD" },
        }),
      ],
      nextCursor: null,
      summary: fullSummary({ count: 2, spentMinor: 6710 }),
    };

    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 2, spentMinor: 6710 },
      nextCursor: "cursor-1",
      extraRoutes: {
        "GET /api/v1/transactions?cursor=cursor-1": {
          status: 200,
          body: olderPage,
        },
        "PATCH /api/v1/transactions/txn-2": {
          status: 200,
          body: expenseFixture({
            id: "txn-2",
            description: "Grab — taxi",
            occurredOn: "2026-07-17",
            amount: { amountMinor: 1480, currency: "SGD" },
          }),
        },
      },
    });

    fireEvent.click(await screen.findByRole("button", { name: /load older/i }));
    fireEvent.click(await screen.findByRole("button", { name: /Grab/i }));

    fireEvent.change(screen.getByLabelText(/description/i), {
      target: { value: "Grab — taxi" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    expect(await screen.findByText("Grab — taxi")).toBeInTheDocument();
    expect(screen.queryByText("Grab")).not.toBeInTheDocument();
  });

  // Same root cause as the test above, the delete half: a row deleted while
  // sitting on an already-loaded older page must actually disappear, not
  // keep showing a transaction that no longer exists.
  it("removes a row from an already-loaded older page once it is deleted", async () => {
    const olderPage = {
      transactions: [
        expenseFixture({
          id: "txn-2",
          description: "Grab",
          occurredOn: "2026-07-17",
          amount: { amountMinor: 1480, currency: "SGD" },
        }),
      ],
      nextCursor: null,
      summary: fullSummary({ count: 2, spentMinor: 6710 }),
    };

    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 2, spentMinor: 6710 },
      nextCursor: "cursor-1",
      extraRoutes: {
        "GET /api/v1/transactions?cursor=cursor-1": {
          status: 200,
          body: olderPage,
        },
        "DELETE /api/v1/transactions/txn-2": { status: 204, body: undefined },
      },
    });

    fireEvent.click(await screen.findByRole("button", { name: /load older/i }));
    fireEvent.click(await screen.findByRole("button", { name: /Grab/i }));
    fireEvent.click(screen.getByRole("button", { name: /delete/i }));
    fireEvent.click(screen.getByRole("button", { name: /yes, delete/i }));

    await waitFor(() =>
      expect(screen.queryByText("Grab")).not.toBeInTheDocument(),
    );
  });

  // GET /transactions is money AND owner-gated, identically to GET /goals
  // and GET /bills (router.go's own `txn` group). Found during Bills' Task
  // 18 walk: this page had no branch at all for that routine 403 -- not
  // even a test to have been fooled by, the same gap BillsPage.tsx and
  // BudgetPage.tsx both had (docs/LEARNING.md pattern 1). Mirrors
  // GoalsPage.test.tsx's own pair in shape.
  it("a 403 from GET /transactions renders the owner-only explanation, not the generic load error", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/categories": { status: 200, body: { categories: [] } },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [accountFixture()] } },
      "GET /api/v1/transactions": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Only an owner may do that." } },
      },
    });

    renderWithRouter(<TransactionsPage />);

    const explanation = await screen.findByTestId("transactions-owner-only");
    expect(explanation).toHaveTextContent("Owner only");
    expect(explanation).toHaveTextContent("Transactions is visible to the household owner.");
    expect(screen.queryByTestId("transactions-load-error")).not.toBeInTheDocument();
  });

  it("a non-403 failure from GET /transactions renders the generic load error, not the owner-only explanation", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/categories": { status: 200, body: { categories: [] } },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [accountFixture()] } },
      "GET /api/v1/transactions": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    renderWithRouter(<TransactionsPage />);

    expect(await screen.findByTestId("transactions-load-error")).toHaveTextContent(
      "Couldn't load your transactions.",
    );
    expect(screen.queryByTestId("transactions-owner-only")).not.toBeInTheDocument();
  });
});
