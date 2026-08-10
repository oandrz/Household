// Follows BillModal.test.tsx's own conventions: renders the real <BillsPage />
// rather than <MarkPaidModal /> in isolation (Goals shipped a modal with no
// screen that ever mounted it, and a whole task's review missed the gap --
// docs/LEARNING.md pattern 15), stubFetchRoutes for every request, literal
// strings throughout rather than BILL_COPY's own exports (BudgetPage.test.tsx's
// own convention: importing the copy module would make an assertion
// tautological against a typo in that same module), and fireEvent/findBy*/
// waitFor for the async gaps a real mount always has.
//
// System time is faked to 2026-08-09 (BillsPage.test.tsx's own convention)
// so "Paid on" always prefills the same date regardless of the real
// calendar the test runner happens to have.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BillsPage } from "./BillsPage";
import type { Bill, BillPayment, BillsResponse } from "./billSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

const ACCOUNTS = {
  status: 200,
  body: {
    accounts: [
      {
        id: "acct-1",
        nickname: "OCBC Everyday",
        type: "cash",
        ownerMembershipId: null,
        ownerName: null,
        balance: { amountMinor: 500000, currency: "SGD" },
        countTowardNetWorth: true,
        visibleToLimitedMembers: false,
        archivedAt: null,
      },
    ],
  },
};

function billFixture(overrides: Partial<Bill> = {}): Bill {
  return {
    id: "bill-1",
    name: "StarHub",
    amountMinor: 6800,
    currency: "SGD",
    cadence: "monthly",
    nextDue: "2026-08-20",
    categoryId: "cat-1",
    categoryName: "Utilities",
    payFromAccountId: "acct-1",
    accountName: "DBS Everyday",
    paidByMembershipId: "mem-1",
    autopay: false,
    isSubscription: false,
    overdue: false,
    dueSoon: true,
    settled: false,
    archivedAt: null,
    ...overrides,
  };
}

function paymentFixture(overrides: Partial<BillPayment> = {}): BillPayment {
  return {
    id: "payment-1",
    billId: "bill-1",
    billName: "StarHub",
    dueOn: "2026-08-05",
    paidOn: "2026-08-05",
    amountMinor: 6800,
    currency: "SGD",
    autopay: false,
    ...overrides,
  };
}

function billsResponseFixture(bills: Bill[], paidThisMonth: BillPayment[] = []): BillsResponse {
  const live = bills.filter((b) => b.archivedAt === null);
  return {
    bills,
    paidThisMonth,
    summary: {
      currency: "SGD",
      dueThisMonthMinor: 0,
      paidSoFarMinor: 0,
      nextDue: null,
      autopayCount: live.filter((b) => b.autopay).length,
      billCount: live.length,
      subscriptionsMonthlyMinor: 0,
      subscriptionsAnnualMinor: 0,
      excludedNoRate: 0,
    },
  };
}

function renderBillsPage(
  bills: Bill[],
  paidThisMonth: BillPayment[] = [],
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    // BillsPage fetches its own accounts (a bill is paid FROM one, so with
    // none both Add-bill entry points are disabled -- BillsPage.tsx's own
    // comment). Registered even though nothing here asserts on it:
    // stubFetchRoutes throws on an unregistered request, react-query swallows
    // that throw into `accounts.isError`, and every test in this file would
    // stay green while running against a failed accounts query rather than
    // the ordinary path.
    "GET /api/v1/accounts": ACCOUNTS,
    "GET /api/v1/bills": { status: 200, body: billsResponseFixture(bills, paidThisMonth) },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<BillsPage />) };
}

// Only the writes matter for most of these assertions -- BillModal.test.tsx's
// own mutatingCalls helper, restated here since GET /currencies and GET
// /bills fire on every mount regardless of what a given test does next.
function mutatingCalls(fetchMock: ReturnType<typeof stubFetchRoutes>): string[] {
  return fetchMock.mock.calls
    .filter(([, init]) => (init?.method ?? "GET").toUpperCase() !== "GET")
    .map(([input, init]) => `${(init?.method ?? "GET").toUpperCase()} ${String(input)}`);
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-09T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  // Restores window.confirm after the one test below that spies on it --
  // unstubAllGlobals only undoes vi.stubGlobal, not vi.spyOn.
  vi.restoreAllMocks();
});

describe("MarkPaidModal", () => {
  // Requirement 1 of the task brief: the amount is prefilled from the bill
  // and editable, and an edited amount posts unchanged by the prefill --
  // utilities vary month to month, which is the whole reason this is a modal
  // rather than a one-click button.
  it("prefills the bill's amount and today's date; an edited amount posts unchanged by the prefill", async () => {
    const bill = billFixture();
    let postBody: unknown;
    const { fetchMock } = renderBillsPage([bill], [], {
      "POST /api/v1/bills/bill-1/pay": {
        status: 200,
        body: { payment: paymentFixture(), bill },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Mark StarHub paid" }));

    expect(await screen.findByLabelText("Amount")).toHaveValue("68.00");
    expect(screen.getByLabelText("Paid on")).toHaveValue("2026-08-09");
    // Pay-from is shown, read-only -- decision 7: a bill's currency (and
    // therefore its pay-from account) is fixed on the bill, never re-chosen
    // at the moment of paying it.
    expect(screen.getByText("DBS Everyday")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "70.50" } });
    fireEvent.click(screen.getByRole("button", { name: "Mark paid" }));

    await waitFor(() => expect(postBody).toEqual({ amountMinor: 7050, paidOn: "2026-08-09" }));
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/bills/bill-1/pay"]);
  });

  // Requirement 2: the modal names what it will do -- write an expense --
  // at the point of clicking, naming the bill so the ledger row it writes
  // (described with the bill's own name) is recognisable as a possible
  // duplicate of a hand-entered transaction (spec decision 1's own accepted
  // cost). Visible the moment the modal opens, before any submit.
  it("names writing an expense to the ledger, with the bill's own name, before the household ever submits", async () => {
    const bill = billFixture({ name: "StarHub" });
    const { fetchMock } = renderBillsPage([bill]);

    fireEvent.click(await screen.findByRole("button", { name: "Mark StarHub paid" }));

    const sentence = await screen.findByText(/writes an expense to the ledger/i);
    expect(sentence).toHaveTextContent('"StarHub"');
    // Nothing posted yet -- the sentence is there before any write is asked
    // for, not composed after the fact from the response.
    expect(mutatingCalls(fetchMock)).toEqual([]);
  });

  // Requirement 3: undo sits behind an in-page confirmation, never
  // window.confirm -- GoalContributionsPanel.tsx's own ContributionRow is
  // the pattern. Asserts the stronger claim, not just that a confirm UI
  // exists: the DELETE request itself waits for the second click.
  it("undo sits behind an in-page confirmation, never window.confirm", async () => {
    const confirmSpy = vi.spyOn(window, "confirm");
    const bill = billFixture({ id: "bill-2", name: "Insurance", nextDue: "2026-08-20" });
    const payment = paymentFixture({ id: "payment-1", billId: "bill-1", billName: "Netflix" });
    const { fetchMock } = renderBillsPage([bill], [payment], {
      "DELETE /api/v1/bills/bill-1/payments/payment-1": { status: 204, body: undefined },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Undo Netflix payment" }));

    expect(await screen.findByText("This removes the expense it wrote to the ledger.")).toBeInTheDocument();
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mutatingCalls(fetchMock)).toEqual([]);

    fireEvent.click(screen.getByRole("button", { name: "Undo payment" }));

    await waitFor(() =>
      expect(mutatingCalls(fetchMock)).toEqual(["DELETE /api/v1/bills/bill-1/payments/payment-1"]),
    );
  });

  // Requirement 4: undo on an older payment surfaces the server's 409
  // inline, naming the payment that can be undone -- not a generic failure.
  // Two payment rows, not one (ContributionRow.tsx's own reasoning: a 409 on
  // one row must read next to that row, not detached under whichever row
  // happens to be last) -- this is what a single-row test could never catch,
  // since it would have nowhere else the error could wrongly land.
  it("undo on an older payment surfaces the server's 409 inline, naming the undoable payment, scoped to that row alone", async () => {
    const bill = billFixture({ id: "bill-x", name: "Insurance", nextDue: "2026-08-20" });
    const newer = paymentFixture({ id: "payment-a", billId: "bill-a", billName: "Netflix", dueOn: "2026-08-05" });
    const older = paymentFixture({ id: "payment-b", billId: "bill-b", billName: "Spotify", dueOn: "2026-08-02" });

    renderBillsPage([bill], [newer, older], {
      "DELETE /api/v1/bills/bill-b/payments/payment-b": {
        status: 409,
        body: {
          error: {
            code: "BILL_PAYMENT_NOT_LATEST",
            message: "Only the most recent payment, due 2026-08-10, can be undone.",
            details: { undoableDueOn: "2026-08-10" },
          },
        },
      },
    });

    const paidSection = await screen.findByTestId("bills-paid-this-month");
    const rows = within(paidSection).getAllByTestId("bill-row");
    expect(rows).toHaveLength(2);

    fireEvent.click(within(rows[1]).getByRole("button", { name: "Undo Spotify payment" }));
    fireEvent.click(within(rows[1]).getByRole("button", { name: "Undo payment" }));

    const alert = await within(rows[1]).findByRole("alert");
    expect(alert).toHaveTextContent("Only the most recent payment, due 2026-08-10, can be undone.");
    // Not on the other row, and not the generic fallback -- the server's own
    // sentence is what must show, not a client-composed substitute.
    expect(within(rows[0]).queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText("Something went wrong. Please try again.")).not.toBeInTheDocument();
  });

  // A refusal's own message can go stale: two payments on the SAME bill,
  // due in the same month (a very-overdue bill caught up twice). Undoing
  // the older one first is refused, naming the newer one as the undoable
  // payment -- correct at that moment. Undoing the newer one next succeeds,
  // which makes the older payment THE bill's own most recent remaining one
  // -- so the first message ("only the payment due on the newer date can be
  // undone") is no longer true the instant the newer payment stops existing.
  // A stale copy of that sentence left sitting under the older row would
  // tell a household it still can't do the thing it can now do.
  it("a successful undo clears a stale refusal left on another row of the same bill", async () => {
    const older = paymentFixture({ id: "payment-older", billId: "bill-1", billName: "Property tax", dueOn: "2026-08-02" });
    const newer = paymentFixture({ id: "payment-newer", billId: "bill-1", billName: "Property tax", dueOn: "2026-08-20" });
    const bill = billFixture({ id: "bill-2", name: "Insurance", nextDue: "2026-08-25" });

    const { fetchMock } = renderBillsPage([bill], [newer, older], {
      "DELETE /api/v1/bills/bill-1/payments/payment-older": {
        status: 409,
        body: {
          error: {
            code: "BILL_PAYMENT_NOT_LATEST",
            message: "Only the most recent payment, due 2026-08-20, can be undone.",
            details: { undoableDueOn: "2026-08-20" },
          },
        },
      },
      "DELETE /api/v1/bills/bill-1/payments/payment-newer": { status: 204, body: undefined },
    });

    const paidSection = await screen.findByTestId("bills-paid-this-month");
    const rows = within(paidSection).getAllByTestId("bill-row");
    expect(rows).toHaveLength(2);

    // paidThisMonth is fed in as [newer, older], and BillsPage preserves
    // that order (never re-sorts) -- so row 0 is the newer payment, row 1
    // the older one.
    const newerRow = rows[0];
    const olderRow = rows[1];

    // Fail undoing the older payment first -- its own row now carries the
    // "only the newer one can be undone" message.
    fireEvent.click(within(olderRow).getByRole("button", { name: "Undo Property tax payment" }));
    fireEvent.click(within(olderRow).getByRole("button", { name: "Undo payment" }));
    await within(olderRow).findByRole("alert");

    // Succeed undoing the newer payment -- the fact the older row's message
    // was built on (a newer payment exists and is the only undoable one) is
    // now false.
    fireEvent.click(within(newerRow).getByRole("button", { name: "Undo Property tax payment" }));
    fireEvent.click(within(newerRow).getByRole("button", { name: "Undo payment" }));

    // Waiting on the mutating call itself (fetchMock.mock.calls) proves only
    // that the DELETE was DISPATCHED -- apiFetch's own fetch() call happens
    // synchronously, before mutateAsync's promise (and the onSuccess ->
    // invalidateBillsAndLedger -> refetch chain it awaits) has settled.
    // Waiting for the row to show its plain trigger again instead proves the
    // FULL chain landed: `finally` only resets confirmingPaymentId once
    // mutateAsync itself resolves, which is after that refetch -- and it is
    // handleConfirmUndo's own explicit clearUndoErrors() call that clears the
    // stale refusal, not a derived effect (BillsPage.tsx's own comment
    // records both effect shapes that were tried first and why neither
    // fired).
    await within(newerRow).findByRole("button", { name: "Undo Property tax payment" });
    expect(mutatingCalls(fetchMock)).toEqual([
      "DELETE /api/v1/bills/bill-1/payments/payment-older",
      "DELETE /api/v1/bills/bill-1/payments/payment-newer",
    ]);
    // The older row is still on screen (its own undo never succeeded) but
    // must no longer carry the now-false refusal.
    expect(within(olderRow).queryByRole("alert")).not.toBeInTheDocument();
  });

  // The sibling of the test above, in the OTHER write path that moves the
  // same fact: every successful MarkPaid writes a new payment for the bill
  // and advances next_due (bill.go), which -- exactly like a successful
  // UndoPayment -- changes which payment is that bill's own most recent.
  // A stored BILL_PAYMENT_NOT_LATEST message is just as stale after a
  // Mark-paid success as after an Undo success; nothing about which write
  // path caused the change is part of what makes the message true or false.
  it("a successful mark-paid also clears a stale refusal left on a payment of the same bill", async () => {
    const older = paymentFixture({ id: "payment-older", billId: "bill-1", billName: "Property tax", dueOn: "2026-08-02" });
    const newer = paymentFixture({ id: "payment-newer", billId: "bill-1", billName: "Property tax", dueOn: "2026-08-20" });
    // The same bill, still carrying a THIRD, not-yet-paid occurrence -- a
    // bill can have payment history behind it while still having a live
    // `nextDue` in front of it; that unpaid occurrence is what Mark paid
    // targets below.
    const bill = billFixture({ id: "bill-1", name: "Property tax", nextDue: "2026-08-25", dueSoon: true });

    const { fetchMock } = renderBillsPage([bill], [newer, older], {
      "DELETE /api/v1/bills/bill-1/payments/payment-older": {
        status: 409,
        body: {
          error: {
            code: "BILL_PAYMENT_NOT_LATEST",
            message: "Only the most recent payment, due 2026-08-20, can be undone.",
            details: { undoableDueOn: "2026-08-20" },
          },
        },
      },
      "POST /api/v1/bills/bill-1/pay": {
        status: 200,
        body: {
          payment: paymentFixture({ id: "payment-newest", billId: "bill-1", billName: "Property tax", dueOn: "2026-08-25" }),
          bill: { ...bill, nextDue: "2026-09-25" },
        },
      },
    });

    const paidSection = await screen.findByTestId("bills-paid-this-month");
    const rows = within(paidSection).getAllByTestId("bill-row");
    const olderRow = rows[1];

    fireEvent.click(within(olderRow).getByRole("button", { name: "Undo Property tax payment" }));
    fireEvent.click(within(olderRow).getByRole("button", { name: "Undo payment" }));
    await within(olderRow).findByRole("alert");

    // Mark paid the bill's own still-unpaid THIRD occurrence, not undo --
    // the other write path that moves MAX(due_on) for this same bill.
    fireEvent.click(await screen.findByRole("button", { name: "Mark Property tax paid" }));
    fireEvent.click(await screen.findByRole("button", { name: "Mark paid" }));

    // The mutating call itself proves only that the POST was dispatched --
    // apiFetch's own fetch() fires synchronously, before markPaid.mutateAsync
    // (and the onSuccess -> invalidateBillsAndLedger -> refetch chain it
    // awaits) has settled. Waiting for the modal to actually close instead
    // proves the FULL chain landed: MarkPaidModal's own onPaid/onClose only
    // fire once mutateAsync resolves, which is after that same refetch --
    // and onPaid is where BillsPage calls clearUndoErrors(), explicitly, at
    // the second of the two write sites that move MAX(due_on).
    await waitFor(() => expect(screen.queryByLabelText("Paid on")).not.toBeInTheDocument());
    expect(mutatingCalls(fetchMock)).toEqual([
      "DELETE /api/v1/bills/bill-1/payments/payment-older",
      "POST /api/v1/bills/bill-1/pay",
    ]);
    expect(within(olderRow).queryByRole("alert")).not.toBeInTheDocument();
  });

  // Every refusal MarkPaid can return -- BILL_ARCHIVED, BILL_SETTLED,
  // ACCOUNT_ARCHIVED, the already-paid 409, an unparseable date -- flows
  // through the identical apiErrorMessage pass-through (MarkPaidModal.tsx's
  // own catch has no per-code branch). One representative case stands for
  // all five rather than enumerating the whole table: BILL_ARCHIVED, since
  // its own wording ("Restore it before marking a payment") is the one the
  // task brief singles out as needing to read as an instruction, not a bare
  // permission refusal.
  it("a refusal (e.g. BILL_ARCHIVED) renders the server's own sentence inline and keeps the modal open", async () => {
    const bill = billFixture();
    const { fetchMock } = renderBillsPage([bill], [], {
      "POST /api/v1/bills/bill-1/pay": {
        status: 422,
        body: {
          error: {
            code: "BILL_ARCHIVED",
            message: "This bill is archived. Restore it before marking a payment.",
          },
        },
      },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Mark StarHub paid" }));
    fireEvent.click(await screen.findByRole("button", { name: "Mark paid" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This bill is archived. Restore it before marking a payment.",
    );
    // Still open, not silently closed on a refusal -- the field the
    // household just filled in is still there to retry once it applies.
    expect(screen.getByLabelText("Amount")).toBeInTheDocument();
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/bills/bill-1/pay"]);
  });

  // Mark paid is reachable from every LIVE row (billCopy.ts's own comment on
  // why settled is deliberately not excluded client-side) but never from an
  // archived one -- that row's own action is Restore, and writeMarkPaidError's
  // own BILL_ARCHIVED message already tells a household to use it.
  it("Mark paid is absent from an archived row; Restore is its only action", async () => {
    const live = billFixture({ id: "bill-1", name: "StarHub" });
    const archived = billFixture({
      id: "bill-4",
      name: "Old gym membership",
      archivedAt: "2026-06-01T00:00:00Z",
      dueSoon: false,
      nextDue: "2026-05-01",
    });
    renderBillsPage([live], [], {
      "GET /api/v1/bills?include_archived=true": {
        status: 200,
        body: billsResponseFixture([live, archived]),
      },
    });

    await screen.findByText("StarHub");
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));

    const archivedSection = await screen.findByTestId("bills-archived-section");
    expect(
      within(archivedSection).queryByRole("button", { name: "Mark Old gym membership paid" }),
    ).not.toBeInTheDocument();
    expect(
      within(archivedSection).getByRole("button", { name: "Restore Old gym membership" }),
    ).toBeInTheDocument();
  });

  // The nested-button hazard: a live bill row is itself role="button" with
  // its own onClick opening BillModal in edit mode. Mark paid's own button
  // must stop that click from bubbling, or clicking it would open BOTH
  // modals at once.
  it("clicking Mark paid opens the pay modal only -- the click does not bubble into the row's own edit-modal handler", async () => {
    const bill = billFixture();
    renderBillsPage([bill]);

    fireEvent.click(await screen.findByRole("button", { name: "Mark StarHub paid" }));

    expect(await screen.findByLabelText("Paid on")).toBeInTheDocument();
    // BillModal's own title renders synchronously the instant it mounts,
    // before its accounts/categories/members queries ever settle (its own
    // "Loading…" branch) -- so this is present the moment onEdit fires, not
    // only once the form itself has rendered. Checking for "Bill name"
    // instead would pass even if onEdit HAD fired, since BillModal's form
    // only appears after that async gap; this is the marker that actually
    // proves the edit modal did not also open.
    expect(screen.queryByRole("heading", { name: "Edit bill" })).not.toBeInTheDocument();
  });

  // The keydown half of the same hazard: Enter/Space on Mark paid must not
  // bubble into the row's own onKeyDown either, which independently opens
  // the edit modal on Enter/Space (BillRow.tsx's own clickable branch).
  it("a keydown on Mark paid does not bubble into the row's own onKeyDown", async () => {
    const bill = billFixture();
    renderBillsPage([bill]);

    const trigger = await screen.findByRole("button", { name: "Mark StarHub paid" });
    fireEvent.keyDown(trigger, { key: "Enter", code: "Enter" });

    // Same synchronous marker as the click test above -- BillModal's title
    // would be on screen immediately if the keydown had bubbled to the
    // row's own onKeyDown, regardless of its own async data still loading.
    expect(screen.queryByRole("heading", { name: "Edit bill" })).not.toBeInTheDocument();
  });
});
