// Every user-visible string on Overview, in a plain .ts module for the same
// reason features/money/copy.ts is one: eslint's
// react-refresh/only-export-components rule never has to think about a file
// mixing components with other exports.
export const OVERVIEW_COPY = {
  title: "Overview",

  // Shown to a member whose capabilities do not include money. Not an error
  // and not an empty card: nothing is broken, this household has simply not
  // shared its money with them.
  noMoneyAccess: "You don't have access to Money in this household.",

  // The third render shape, and the one no test caught until a browser walk
  // did. A limited member *with* money reaches this page, gets no summary
  // (the server omits it rather than zeroing it), no budget card and no
  // checklist -- so without this they saw a heading and nothing else. Says
  // what is true and where they can still go, rather than leaving a blank
  // page that reads as broken.
  limitedHeading: "Money",
  limitedNoAmounts:
    "Amounts are hidden for your account. The accounts shared with you are in Finances.",
  limitedGo: "Go to Finances",

  budgetHeading: "This month",
  // The never-budgeted state. The wording matches BudgetPage's own empty
  // state, which is the screen this card links to.
  budgetNone: "No budget set yet",
  budgetSetUp: "Set a budget",
  budgetUsed: (percent: number) => `${percent}% used`,
  budgetOf: (spent: string, budgeted: string) => `${spent} of ${budgeted}`,

  setupHeading: "Finish setting up",
  setupProgress: (done: number, total: number) => `${done} of ${total} done`,
  setupHousehold: "Create your household",
  setupAccount: "Add an account",
  // The month is read at render time, never written into this string -- a
  // literal "July" is wrong for eleven months of the year.
  setupBudget: (monthName: string) => `Set a budget for ${monthName}`,
  setupGo: "Set up",

  quickAdd: "+ Add",
  quickAddTransaction: "Transaction",
  quickAddAccount: "Account",
  // Transactions attach to an account. With none, the entry would open a
  // modal whose account dropdown is empty -- the dead end TransactionsPage's
  // own comment refuses.
  quickAddNeedsAccount: "Add an account first",
} as const;

