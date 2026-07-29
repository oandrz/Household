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

  // Sibling of AccountModal's own "names the currency, not the figure" test:
  // logging an expense against an IDR account (bca, in these fixtures -- and
  // the design's own "Transfer to BCA" row) with a figure that still has
  // cents must not tell the person to type back the exact thing they typed.
  it("names the currency, not the figure, when the account takes no cents", () => {
    renderModal();

    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: "Warung" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "50000.50" } });
    fireEvent.change(screen.getByLabelText(/account/i), { target: { value: "bca" } });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    expect(
      screen.getByText("IDR doesn't use cents. Remove the decimal point."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Enter an amount, like 52.30.")).not.toBeInTheDocument();
  });

  // The smaller symptom review finding 2 named alongside the main bug:
  // receivedAmountTouched starts true when editing (so an already-stored
  // transfer's figure is never silently recomputed), but that reasoning does
  // not apply the first time editing a non-transfer switches its kind to
  // Transfer at all -- the brief says the field is "always present, prefilled
  // with the amount sent", and dbs (SGD, this transaction's own account) and
  // ocbc (SGD, the next account in the fixture list) share a currency, so
  // this is the optional/mirrored case, not the required one.
  it("prefills amount received even when editing a non-transfer switches it into one", () => {
    renderModal({ initial: EXPENSE_TRANSACTION });

    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));

    expect(screen.getByLabelText(/amount received/i)).toHaveValue("52.30");
  });

  it("prefills every field from an existing transaction for editing", () => {
    renderModal({ initial: EXPENSE_TRANSACTION });

    expect(screen.getByLabelText(/description/i)).toHaveValue("Cold Storage");
    expect(screen.getByLabelText(/^amount/i)).toHaveValue("52.30");
    expect(screen.getByLabelText(/account/i)).toHaveValue("dbs");
  });

  // Review finding 1: categoryId used to be set once and only ever changed by
  // the select's own onChange -- switching kind changed which categories are
  // *displayed* but never touched the held id. Picking Groceries on Expense,
  // then switching to Income, left "cat-groceries" sitting in state while the
  // select itself showed nothing selected; both submit branches sent it
  // verbatim, and the backend refused it with a rejection that pointed at a
  // field that looked empty.
  it("does not carry a stale category id across a kind switch", async () => {
    const { onSubmit } = renderModal({ onSubmit: vi.fn().mockResolvedValue(undefined) });

    const categorySelect = await screen.findByLabelText(/category/i);
    // The categories query resolves asynchronously; selecting "cat-groceries"
    // before its <option> exists would be a silent no-op, same as selecting a
    // not-yet-registered account -- so this waits for the option itself, not
    // just the select, mirroring AccountModal.test.tsx's own IDR-option wait.
    await screen.findByRole("option", { name: "Groceries" });
    fireEvent.change(categorySelect, { target: { value: "cat-groceries" } });
    expect(categorySelect).toHaveValue("cat-groceries");

    fireEvent.click(screen.getByRole("button", { name: "Income" }));
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: "Salary" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "1000.00" } });
    fireEvent.change(screen.getByLabelText(/account/i), { target: { value: "dbs" } });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0][0].categoryId).toBeNull();
  });

  // Review finding 2: receivedAmountTouched used to gate both "mirror the
  // amount sent" and "clear + require on a currency mismatch". Typing a bank
  // fee while dbs -> ocbc (both SGD) marks the field touched; changing the
  // destination to bca (IDR) afterward must still clear it, because a figure
  // typed under one currency is never valid to keep once the assumption
  // changes -- left alone, "120.00" would be sent as receivedAmountMinor:
  // 12000, silently reinterpreted as 120 *rupiah* instead of the 120 Singapore
  // dollars it was typed as.
  it("clears a received amount typed under one currency once the destination currency changes", () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderModal({ onSubmit });

    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    fireEvent.change(screen.getByLabelText(/from account/i), { target: { value: "dbs" } });
    fireEvent.change(screen.getByLabelText(/to account/i), { target: { value: "ocbc" } });
    fireEvent.change(screen.getByLabelText(/amount received/i), { target: { value: "120.00" } });
    expect(screen.getByLabelText(/amount received/i)).toHaveValue("120.00");

    fireEvent.change(screen.getByLabelText(/to account/i), { target: { value: "bca" } });

    const receivedField = screen.getByLabelText(/amount received/i);
    expect(receivedField).toHaveValue("");
    expect(receivedField).toBeRequired();

    // The stale figure must not be submittable by accident either: without
    // retyping anything, submitting must not carry the discarded "120.00"
    // forward. required+empty means jsdom's own constraint validation
    // refuses the submit event before this component's handler ever runs
    // (the same as a real browser), so onSubmit not being called is exactly
    // what proves the old figure went nowhere -- the field-error path this
    // component's handleSubmit also has for this case is unreachable here for
    // that reason, not evidence of anything.
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: "Transfer to BCA" } });
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "500.00" } });
    fireEvent.click(screen.getByRole("button", { name: /save transaction/i }));

    expect(onSubmit).not.toHaveBeenCalled();
  });
});
