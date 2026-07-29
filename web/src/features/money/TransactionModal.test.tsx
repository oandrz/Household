import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { TransactionModal, type TransactionFormValues } from "./TransactionModal";
import type { Account } from "./schemas";
import type { Category, Transaction } from "./transactionSchemas";

afterEach(() => vi.unstubAllGlobals());

// Full Account objects, not the brief's flattened { id, nickname, currency }
// guess -- schemas.ts's accountSchema nests currency under `balance`, and a
// fixture missing every other required field (type, ownerMembershipId,
// countTowardNetWorth, archivedAt, ...) would only work by accident with a
// component that never reads them. "ocbc" exists here specifically so the
// same-currency half of the received-amount test below selects a real,
// different account rather than silently no-op'ing on a destination that was
// never registered -- the brief's own version selected "ocbc" without ever
// defining it.
function account(overrides: Partial<Account> & Pick<Account, "id" | "nickname" | "balance">): Account {
  return {
    type: "cash",
    ownerMembershipId: null,
    ownerName: null,
    balanceAsOf: "2026-07-01",
    countTowardNetWorth: true,
    visibleToLimitedMembers: false,
    archivedAt: null,
    ...overrides,
  };
}

const DBS = account({ id: "dbs", nickname: "DBS Everyday", balance: { amountMinor: 500000, currency: "SGD" } });
const OCBC = account({ id: "ocbc", nickname: "OCBC 360", balance: { amountMinor: 200000, currency: "SGD" } });
const BCA = account({ id: "bca", nickname: "BCA Tahapan", balance: { amountMinor: 900000000, currency: "IDR" } });
const ACCOUNTS = [DBS, OCBC, BCA];

const MEMBERS = [
  { id: "m1", name: "Christine" },
  { id: "m2", name: "Andreas" },
];

const CATEGORIES: Category[] = [
  { id: "cat-groceries", name: "Groceries", kind: "expense" },
  { id: "cat-salary", name: "Salary", kind: "income" },
];

const EXPENSE_TRANSACTION: Transaction = {
  id: "t1",
  kind: "expense",
  occurredOn: "2026-07-18",
  description: "Cold Storage",
  categoryId: null,
  categoryName: null,
  paidByMembershipId: null,
  paidByName: null,
  fromAccountId: "dbs",
  fromAccountName: "DBS Everyday",
  toAccountId: null,
  toAccountName: null,
  amount: { amountMinor: 5230, currency: "SGD" },
  receivedAmount: null,
  beforeFromAccountOpeningBalance: false,
  beforeToAccountOpeningBalance: null,
};

// A small local harness, following AccountModal.test.tsx and
// AccountsPanel.test.tsx's own plain QueryClientProvider render (no router:
// this modal reaches no <Link>). fireEvent, not userEvent -- the brief's own
// sketch imported "@testing-library/user-event", which is not one of this
// project's dependencies (only @testing-library/react and jest-dom are);
// every other modal test here already uses fireEvent, so this follows suit
// rather than adding a new package mid-task.
//
// useCategories() fires a real query on every mount, so its route is always
// registered here -- stubFetchRoutes throws on an unregistered route rather
// than silently letting a fetch through unmatched.
function renderModal(
  props: Partial<{
    onSubmit: ReturnType<typeof vi.fn<(values: TransactionFormValues) => Promise<unknown>>>;
    onDelete: () => Promise<unknown>;
    initial: Transaction;
  }> = {},
  categories: Category[] = CATEGORIES,
) {
  stubFetchRoutes({
    "GET /api/v1/categories": { status: 200, body: { categories } },
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onClose = vi.fn();
  const onSubmit = props.onSubmit ?? vi.fn().mockResolvedValue(undefined);
  return {
    onClose,
    onSubmit,
    ...render(
      <QueryClientProvider client={queryClient}>
        <TransactionModal
          open
          onClose={onClose}
          onSubmit={onSubmit}
          onDelete={props.onDelete}
          initial={props.initial}
          accounts={ACCOUNTS}
          members={MEMBERS}
        />
      </QueryClientProvider>,
    ),
  };
}

describe("TransactionModal", () => {
  it("offers a category for an expense and no category at all for a transfer", () => {
    renderModal();

    expect(screen.getByLabelText(/category/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    expect(screen.queryByLabelText(/category/i)).not.toBeInTheDocument();
    expect(screen.getByLabelText(/from account/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/to account/i)).toBeInTheDocument();
  });

  // The field is optional within one currency -- a bank fee -- and required
  // across two, where there is no honest figure to prefill it with.
  it("requires the amount received only when the two accounts differ in currency", () => {
    renderModal();

    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    fireEvent.change(screen.getByLabelText(/from account/i), { target: { value: "dbs" } });
    // ocbc is a real, distinct SGD account (unlike the brief's original
    // fixture, which never defined it) -- so this actually changes which
    // account is selected, rather than leaving the destination wherever it
    // already defaulted to.
    fireEvent.change(screen.getByLabelText(/to account/i), { target: { value: "ocbc" } });

    const optional = screen.getByLabelText(/amount received/i);
    expect(optional).not.toBeRequired();

    fireEvent.change(screen.getByLabelText(/to account/i), { target: { value: "bca" } });
    const required = screen.getByLabelText(/amount received/i);
    expect(required).toBeRequired();
    // Labelled with the destination's currency, not the household's.
    expect(screen.getByText(/IDR/)).toBeInTheDocument();
  });

  it("prefills amount received with the amount sent, until touched", () => {
    renderModal();

    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    // Exact text, not /^amount/i: once Transfer is showing, "Amount received"
    // also starts with "Amount" and would make that regex ambiguous.
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "120.00" } });
    fireEvent.change(screen.getByLabelText(/from account/i), { target: { value: "dbs" } });
    fireEvent.change(screen.getByLabelText(/to account/i), { target: { value: "ocbc" } });

    expect(screen.getByLabelText(/amount received/i)).toHaveValue("120.00");
  });

  it("sends no currency field at all — the server derives it from the account", async () => {
    const { onSubmit } = renderModal({ onSubmit: vi.fn().mockResolvedValue(undefined) });

    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: "Cold Storage" } });
    fireEvent.change(screen.getByLabelText(/^amount/i), { target: { value: "52.30" } });
    fireEvent.change(screen.getByLabelText(/account/i), { target: { value: "dbs" } });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    // Asserted before inspecting the body -- otherwise "not.toHaveProperty"
    // would pass trivially for a submit handler that was never invoked at
    // all, which is exactly the shape of test that cannot fail.
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const body = onSubmit.mock.calls[0][0];
    expect(body).not.toHaveProperty("currency");
    expect(body).not.toHaveProperty("amountCurrency");
    // Minor units, never a float: 52.30 is 5230 cents.
    expect(body.amountMinor).toBe(5230);
  });

  // Both categories exist in the fixture on purpose: a fixture holding only
  // expense categories would make this filter untestable, since "no income
  // categories shown" would be true whether or not the component actually
  // filters.
  it("only offers categories matching the transaction's kind", async () => {
    renderModal();

    const expenseSelect = await screen.findByLabelText(/category/i);
    await waitFor(() => expect(within(expenseSelect).getByText("Groceries")).toBeInTheDocument());
    expect(within(expenseSelect).queryByText("Salary")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Income" }));
    const incomeSelect = screen.getByLabelText(/category/i);
    await waitFor(() => expect(within(incomeSelect).getByText("Salary")).toBeInTheDocument());
    expect(within(incomeSelect).queryByText("Groceries")).not.toBeInTheDocument();
  });

  it("shows no delete control when adding", () => {
    renderModal();
    expect(screen.queryByRole("button", { name: /delete transaction/i })).not.toBeInTheDocument();
  });

  // Never window.confirm: a native dialog blocks every other browser event,
  // which would freeze Task 19's real-browser walk.
  it("confirms deletion in the page, never with a native confirm dialog", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    const confirmSpy = vi.spyOn(window, "confirm");
    const { onClose } = renderModal({ onDelete, initial: EXPENSE_TRANSACTION });

    fireEvent.click(screen.getByRole("button", { name: /delete transaction/i }));
    expect(onDelete).not.toHaveBeenCalled();
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(screen.getByText(/permanently deleted/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /yes, delete/i }));
    await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("prefills every field from an existing transaction for editing", () => {
    renderModal({ initial: EXPENSE_TRANSACTION });

    expect(screen.getByLabelText(/description/i)).toHaveValue("Cold Storage");
    expect(screen.getByLabelText(/^amount/i)).toHaveValue("52.30");
    expect(screen.getByLabelText(/account/i)).toHaveValue("dbs");
  });
});
