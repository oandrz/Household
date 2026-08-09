// Follows GoalModal.test.tsx/BudgetModal.test.tsx's conventions:
// renderWithRouter for a fresh QueryClient, stubFetchRoutes for every
// request (it throws on anything unregistered), fireEvent + findBy*/waitFor
// for the async gaps a real mount always has, and every input driven through
// fireEvent.change.
//
// BillModal calls useAccounts/useCategories/useHouseholdMembers itself (see
// BillModal.tsx's own header comment on why), so every scenario here
// registers all three GETs even when a test never looks at their data --
// stubFetchRoutes throws on an unregistered request, and the modal fires all
// three the moment it mounts.
//
// The entry-point tests render <BillsPage /> rather than <BillModal />
// directly -- Goals shipped GoalModal with every layer beneath it built and
// no screen that ever mounted it, and a whole task's review missed the gap
// (docs/LEARNING.md pattern 15; closed for goals in commit d1c7248). Asserting
// against BillModal in isolation would prove the component works, never that
// BillsPage.tsx actually opens it.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BillModal } from "./BillModal";
import { BillsPage } from "./BillsPage";
import type { Bill, BillsResponse } from "./billSchemas";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-09T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

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
        nickname: "DBS Everyday",
        type: "cash",
        ownerMembershipId: null,
        ownerName: null,
        balance: { amountMinor: 500000, currency: "SGD" },
        countTowardNetWorth: true,
        visibleToLimitedMembers: false,
        archivedAt: null,
      },
      {
        id: "acct-2",
        nickname: "Jakarta BCA",
        type: "cash",
        ownerMembershipId: null,
        ownerName: null,
        balance: { amountMinor: 2000000, currency: "IDR" },
        countTowardNetWorth: true,
        visibleToLimitedMembers: false,
        archivedAt: null,
      },
    ],
  },
};

const CATEGORIES = {
  status: 200,
  body: {
    categories: [
      { id: "cat-1", name: "Utilities", kind: "expense" },
      { id: "cat-2", name: "Streaming", kind: "expense" },
    ],
  },
};

// GET /household/members answers a bare JSON array (member_handlers.go's
// handleListMembers writes the DTO slice straight to the response, no
// wrapping object) -- AccountModal.test.tsx's own NO_MEMBERS comment on the
// same route.
const MEMBERS = {
  status: 200,
  body: [
    {
      id: "mem-1",
      user: { id: "u1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
      role: "owner",
      capabilities: ["money"],
    },
  ],
};

const BASE_ROUTES = {
  "GET /api/v1/accounts": ACCOUNTS,
  "GET /api/v1/categories": CATEGORIES,
  "GET /api/v1/household/members": MEMBERS,
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

function renderModal(
  props: Partial<Parameters<typeof BillModal>[0]> = {},
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const onClose = vi.fn();
  const onSaved = vi.fn();
  const fetchMock = stubFetchRoutes({ ...BASE_ROUTES, ...extraRoutes });

  renderWithRouter(<BillModal mode="create" onClose={onClose} onSaved={onSaved} {...props} />);

  return { fetchMock, onClose, onSaved };
}

// Only the writes matter for these assertions -- BudgetModal.test.tsx's own
// mutatingCalls helper, restated here since the three GETs above fire on
// every mount regardless of what a given test does next.
function mutatingCalls(fetchMock: ReturnType<typeof stubFetchRoutes>): string[] {
  return fetchMock.mock.calls
    .filter(([, init]) => (init?.method ?? "GET").toUpperCase() !== "GET")
    .map(([input, init]) => `${(init?.method ?? "GET").toUpperCase()} ${String(input)}`);
}

function billsResponseFixture(bills: Bill[]): BillsResponse {
  const live = bills.filter((b) => b.archivedAt === null);
  return {
    bills,
    paidThisMonth: [],
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

function renderBillsPage(bills: Bill[], extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/bills": { status: 200, body: billsResponseFixture(bills) },
    ...BASE_ROUTES,
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<BillsPage />) };
}

describe("BillModal", () => {
  it("opens blank for a new bill and POSTs on save", async () => {
    let postBody: unknown;
    const { fetchMock, onClose, onSaved } = renderModal(undefined, {
      "POST /api/v1/bills": {
        status: 201,
        body: { bill: billFixture({ id: "bill-new" }) },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    expect(await screen.findByLabelText("Bill name")).toHaveValue("");
    expect(screen.getByLabelText("Amount")).toHaveValue("");
    expect(screen.getByLabelText("Repeats")).toHaveValue("monthly");
    expect(screen.getByLabelText("Pay from")).toHaveValue("acct-1");
    expect(screen.getByRole("button", { name: "Add bill" })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Bill name"), { target: { value: "StarHub" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "68.00" } });
    fireEvent.change(screen.getByLabelText("Next due"), { target: { value: "2026-08-20" } });
    fireEvent.change(screen.getByLabelText("Category"), { target: { value: "cat-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add bill" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    expect(postBody).toEqual({
      name: "StarHub",
      amountMinor: 6800,
      cadence: "monthly",
      nextDue: "2026-08-20",
      categoryId: "cat-1",
      payFromAccountId: "acct-1",
      paidByMembershipId: "",
      autopay: false,
      isSubscription: false,
    });
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/bills"]);
  });

  it("opens populated from an existing bill and PATCHes, never sending an archive field", async () => {
    const bill = billFixture();
    let patchBody: unknown;
    const { fetchMock, onClose, onSaved } = renderModal(
      { mode: "edit", bill },
      {
        [`PATCH /api/v1/bills/${bill.id}`]: {
          status: 200,
          body: { bill },
          capture: (body) => {
            patchBody = body;
          },
        },
      },
    );

    expect(await screen.findByLabelText("Bill name")).toHaveValue("StarHub");
    expect(screen.getByLabelText("Amount")).toHaveValue("68.00");
    expect(screen.getByLabelText("Repeats")).toHaveValue("monthly");
    expect(screen.getByLabelText("Next due")).toHaveValue("2026-08-20");
    expect(screen.getByLabelText("Category")).toHaveValue("cat-1");
    expect(screen.getByLabelText("Pay from")).toHaveValue("acct-1");
    expect(screen.getByLabelText("Paid by")).toHaveValue("mem-1");
    expect(screen.getByRole("switch", { name: "On autopay" })).toHaveAttribute("aria-checked", "false");
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    // An exact match pins "no archive field" by construction -- Archiving is
    // deliberately not a patchable field (UpdateBillBody's own comment), so
    // this object must never grow one, whatever it might be called.
    expect(patchBody).toEqual({
      name: "StarHub",
      amountMinor: 6800,
      cadence: "monthly",
      payFromAccountId: "acct-1",
      autopay: false,
      isSubscription: false,
      nextDue: "2026-08-20",
      categoryId: "cat-1",
      paidByMembershipId: "mem-1",
    });
    expect(patchBody).not.toHaveProperty("archivedAt");
    expect(patchBody).not.toHaveProperty("archived");
    expect(mutatingCalls(fetchMock)).toEqual([`PATCH /api/v1/bills/${bill.id}`]);
  });

  // A settled one-off (paid, no next occurrence) has nextDue: null --
  // billDTO's own comment on why that field is never dropped from the DTO.
  // Prefilling the date input from `bill.nextDue ?? today()` would silently
  // turn null into today's date, so an edit that never touches Next due
  // (renaming the bill, say) would still PATCH a nextDue the household never
  // chose and un-settle it -- docs/LEARNING.md pattern 1, in the one field
  // the task brief singles out. The field must render genuinely blank, and a
  // save that leaves it blank must omit nextDue from the patch entirely
  // (useBills.ts's own "absent key round-trips as unchanged" convention).
  it("editing a settled one-off leaves Next due blank and never sends the field", async () => {
    const settled = billFixture({
      cadence: "one_off",
      nextDue: null,
      settled: true,
      dueSoon: false,
      overdue: false,
    });
    let patchBody: unknown;
    const { onSaved } = renderModal(
      { mode: "edit", bill: settled },
      {
        [`PATCH /api/v1/bills/${settled.id}`]: {
          status: 200,
          body: { bill: settled },
          capture: (body) => {
            patchBody = body;
          },
        },
      },
    );

    expect(await screen.findByLabelText("Next due")).toHaveValue("");
    // Settled means nothing to require -- the field's own `required`
    // attribute must not block a save that leaves it exactly as found.
    expect(screen.getByLabelText("Next due")).not.toBeRequired();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(patchBody).not.toHaveProperty("nextDue");
  });

  // Spec decision 3 / the task brief's own rewrite: the design's "Mark as
  // automatically paid — otherwise you'll get a reminder" promises two things
  // this product does not do (nothing pays itself, and the bill-due reminder
  // has never sent anything). This pins the replacement, verbatim, and that
  // the design's own wording is gone, not merely that a wording exists.
  it("the autopay toggle's copy is the rewritten one, not the design's", async () => {
    renderModal();

    await screen.findByLabelText("Bill name");
    expect(
      screen.getByText("The bank pays this one — we'll still ask you to confirm it went out."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Mark as automatically paid/)).not.toBeInTheDocument();
    expect(screen.queryByText(/you'll get a reminder/)).not.toBeInTheDocument();
  });

  it("a 409 on a name held by an archived bill offers Restore instead of a bare error", async () => {
    const { fetchMock, onClose, onSaved } = renderModal(undefined, {
      "POST /api/v1/bills": {
        status: 409,
        body: {
          error: {
            code: "BILL_NAME_TAKEN",
            message: '"StarHub" is the name of an archived bill. Restore it, or choose a different name.',
            details: { archivedBillId: "bill-archived-1" },
          },
        },
      },
      "POST /api/v1/bills/bill-archived-1/restore": {
        status: 200,
        body: { bill: billFixture({ id: "bill-archived-1" }) },
      },
    });

    fireEvent.change(await screen.findByLabelText("Bill name"), { target: { value: "StarHub" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "68.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Add bill" }));

    const restoreButton = await screen.findByRole("button", { name: "Restore" });
    expect(screen.getByRole("alert")).toHaveTextContent("is the name of an archived bill");
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(restoreButton);

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    expect(mutatingCalls(fetchMock)).toEqual([
      "POST /api/v1/bills",
      "POST /api/v1/bills/bill-archived-1/restore",
    ]);
  });

  it("a 409 BILL_NAME_TAKEN against a live bill keeps the modal open with the taken name in the message", async () => {
    const { fetchMock, onClose, onSaved } = renderModal(undefined, {
      "POST /api/v1/bills": {
        status: 409,
        body: { error: { code: "BILL_NAME_TAKEN", message: "A bill with that name already exists." } },
      },
    });

    fireEvent.change(await screen.findByLabelText("Bill name"), { target: { value: "StarHub" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "68.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Add bill" }));

    expect(await screen.findByRole("alert")).toHaveTextContent('"StarHub" is already the name of a bill');
    expect(screen.queryByRole("button", { name: "Restore" })).not.toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/bills"]);
  });

  // Decision 7: a bill has no currency of its own -- it is denominated in
  // its pay-from account's. Re-pointing an existing bill at a
  // different-currency account is refused with a 422 naming both currencies
  // (writeBillCurrencyMismatch, bill_handlers.go). Create mode has no
  // equivalent case: nothing has been chosen yet for a new account to
  // disagree with.
  it("choosing a pay-from account in a different currency shows the server's 422 inline, naming both currencies, and keeps the modal open", async () => {
    const bill = billFixture();
    const { fetchMock, onClose, onSaved } = renderModal(
      { mode: "edit", bill },
      {
        [`PATCH /api/v1/bills/${bill.id}`]: {
          status: 422,
          body: {
            error: {
              code: "BILL_CURRENCY_IMMUTABLE",
              message:
                "This bill is in SGD; that account is in IDR. A bill's currency cannot be changed after it is created.",
              details: { billCurrency: "SGD", accountCurrency: "IDR" },
            },
          },
        },
      },
    );

    await screen.findByLabelText("Bill name");
    fireEvent.change(screen.getByLabelText("Pay from"), { target: { value: "acct-2" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("This bill is in SGD");
    expect(alert).toHaveTextContent("that account is in IDR");
    expect(onClose).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
    expect(mutatingCalls(fetchMock)).toEqual([`PATCH /api/v1/bills/${bill.id}`]);
  });
});

// Goals shipped GoalModal with every layer beneath it built and no screen
// that ever mounted it -- a whole task's review missed the gap
// (docs/LEARNING.md pattern 15). Each of these renders the real BillsPage and
// drives the actual control a household would click, separately, so no
// single passing test could be hiding a still-unwired second or third entry
// point.
describe("BillModal is mounted at all three of BillsPage's entry points", () => {
  it("the header's + Add bill opens the create-bill modal, blank", async () => {
    renderBillsPage([billFixture()]);

    fireEvent.click(await screen.findByTestId("bills-add"));

    expect(await screen.findByLabelText("Bill name")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Add bill" })).toBeInTheDocument();
  });

  it("the empty state's own call to action opens the create-bill modal, blank", async () => {
    renderBillsPage([]);

    fireEvent.click(await screen.findByTestId("bills-create-first"));

    expect(await screen.findByLabelText("Bill name")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Add bill" })).toBeInTheDocument();
  });

  it("clicking a live bill row opens the edit modal, prefilled from that bill", async () => {
    renderBillsPage([billFixture({ id: "b1", name: "StarHub" })]);

    fireEvent.click(await screen.findByTestId("bill-row"));

    expect(await screen.findByLabelText("Bill name")).toHaveValue("StarHub");
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });
});
