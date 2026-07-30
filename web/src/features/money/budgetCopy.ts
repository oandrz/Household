// Copy for the Budget screen, in a plain .ts module for the same reason
// transactionCopy.ts/copy.ts are: eslint's react-refresh/only-export-components
// rule never has to think about a file that mixes components with other
// exports.
export const BUDGET_COPY = {
  title: "Budget",

  daysLeftInMonth: (days: number, month: string) =>
    `${days} day${days === 1 ? "" : "s"} left in ${month}`,

  percentUsed: (percent: number) => `${percent}% used`,

  editBudget: "Edit budget",
  history: "History",

  budgeted: "Budgeted",
  // "so far" is dropped for a month that isn't the current one -- daysLeft
  // reaching 0 is the server's own signal for that (spec's formulas table:
  // days left is 0 for a past month), and "Spent so far" for a month that
  // finished weeks ago reads as if the screen doesn't know it's over.
  spentSoFar: "Spent so far",
  spent: "Spent",
  remaining: "Remaining",
  dailyPace: "Daily pace",
  perDayLeft: "/day left",

  categories: "Categories",
  editCategories: "Edit categories",
  // Matches AccountsPanel.tsx's own "(archived)" marker shape -- one
  // convention for "this still shows because history stays true, but you
  // can't pick it again" across the app.
  archivedMarker: "(archived)",
  overMarker: "· over",

  spendingByPerson: "Spending by person",

  onPaceToSave: (amount: string) => `On pace to save ${amount}`,
  // The over-category sentence, derived from `overCount` (decision: the
  // server's own count is the single source of truth for "how many," so the
  // page never re-derives a second count by filtering the category list --
  // see BudgetPage.tsx's overCategorySentence comment). Deliberately no
  // second sentence about unspent budget moving anywhere -- the design's own
  // "Unspent budget rolls into the Bali trip goal at month end" is the
  // rollover feature spec decision 1 defers to Goals, whole. It does not
  // ship stubbed or dormant, so this copy must never grow that sentence back
  // in from a future copy-paste of the design.
  onlyCategoryOver: (categoryName: string) => `${categoryName} is the only category over.`,
  categoriesOver: (count: number) => `${count} categories are over.`,

  // Same shape as transactionCopy.ts's TRANSACTIONS_COPY.excludedNoRate, but
  // without a currency list -- budgetSchemas.ts's own comment on
  // `excludedNoRate` says why: this screen has no ledger rows to name the
  // currencies against, only the count.
  excludedNoRate: (count: number) =>
    `${count} ${count === 1 ? "transaction is" : "transactions are"} not counted: no exchange rate.`,

  // Task 13 builds the real empty state (the design's "Create your first
  // budget" panel and templates); this is a holding placeholder so a month
  // with no budget row still renders something rather than nothing.
  emptyPlaceholder: "No budget set for this month yet.",

  loadError: "Couldn't load your budget.",
} as const;
