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

  budgetHeading: "This month",
  // The never-budgeted state. The wording matches BudgetPage's own empty
  // state, which is the screen this card links to.
  budgetNone: "No budget set yet",
  budgetSetUp: "Set a budget",
  budgetUsed: (percent: number) => `${percent}% used`,
  budgetOf: (spent: string, budgeted: string) => `${spent} of ${budgeted}`,
} as const;
