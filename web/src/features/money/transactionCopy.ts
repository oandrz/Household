// Copy for the Transactions screen, in a plain .ts module for the same reason
// features/money/copy.ts is: eslint's react-refresh/only-export-components
// rule never has to think about a file that mixes components with other
// exports.
export const TRANSACTIONS_COPY = {
  title: "All transactions",
  backToFinances: "‹ Finances",
  add: "+ Add transaction",
  loadOlder: "Load older transactions ↓",
  spentThisMonth: "Spent this month",
  countInMonth: (count: number, month: string) =>
    `${count} in ${month}`,
  // The design's own banner. It is the promise categories exist to keep.
  categoriesFeedBudget:
    "Every expense's category feeds that category's Budget spend automatically.",

  // First run. A household sees this the day after it adds its first account,
  // so it is a real screen rather than an edge case.
  emptyTitle: "Nothing logged yet.",
  emptyBody: "Add an expense, some income or a transfer, and it will show up here.",
  // Deliberately different from the above: a household that filtered to
  // "Income · Petrol" and saw the first-run panel would think its ledger had
  // been wiped.
  noMatchesTitle: "Nothing matches those filters.",
  noMatchesAction: "Clear filters",

  // The button is disabled rather than opening a modal whose account dropdown
  // is empty -- a dead end reached after four clicks.
  noAccountsYet: "Add an account first, and transactions can attach to it.",

  excludedNoRate: (count: number, currencies: string) =>
    `${count} ${count === 1 ? "transaction is" : "transactions are"} not counted: no exchange rate for ${currencies}.`,

  // Names the account, because a transfer can predate one side's opening
  // balance and not the other's -- "does not move the balance" would be half
  // true without saying which.
  beforeOpeningBalance: (accountName: string) =>
    `Before ${accountName}'s opening balance — it doesn't change that balance.`,

  amountReceived: "Amount received",
  amountReceivedHint: (currency: string) =>
    `What actually arrived, in ${currency}.`,

  // The Log-a-transaction modal (Task 15). One title for both add and edit --
  // AccountModal keeps a single "Account details" title for the same reason:
  // the fields say what's being edited, so a second title would only repeat
  // that.
  logTransaction: "Log a transaction",
  saveTransaction: "Save transaction",
  noCategory: "No category",

  deleteTransaction: "Delete transaction",
  // Said plainly rather than offered as an undo that doesn't exist -- a
  // transaction is hard deleted (transaction_handlers.go's
  // handleDeleteTransaction), so "delete" has to mean it here.
  deleteConfirmBody: "This transaction will be permanently deleted. This can't be undone.",
  deleteConfirmAction: "Yes, delete it",
  deleteCancelAction: "Keep it",
} as const;

// Note for whoever builds the filter UI (Task 16): a malformed filter id in
// the query string gets a 422, with three codes documented nowhere but
// transaction_handlers.go's parseTransactionFilter --
// INVALID_ACCOUNT_FILTER, INVALID_CATEGORY_FILTER, INVALID_PAID_BY_FILTER.
// Named with a `_FILTER` suffix so they don't collide with the body-level
// INVALID_ACCOUNTS / INVALID_CATEGORY codes create/update already use.
// Nothing here needs to *handle* them: the filter UI only ever offers ids the
// server itself returned via useAccounts/useCategories, so a malformed filter
// id should never actually reach the request. Left as a comment rather than
// an exported constant so nothing here is tempted to build handling for a
// case that is not supposed to be reachable.
