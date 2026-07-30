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

  // The empty state (Household Dashboard.dc.html's Budget screen, "Set
  // state" -> "never budgeted"). Task 12 shipped a holding placeholder here;
  // this is the design's real copy, word for word.
  emptyHeadline: (month: string) => `No budget set for ${month} yet`,
  emptyBody:
    "A budget gives every dollar a job. Set a monthly cap per category and Hearth will track spending against it automatically from your linked accounts.",
  createFirstBudget: "Create your first budget",
  startFromTemplate: "Start from a template",

  templateFamilyOfFour: "Family of four",
  // The design's own card reads "10 categories · SGD" -- spec decision 6
  // is explicit that the "· SGD" is seed-data storytelling, not a currency
  // rule, so this takes the household's real currency code as an argument
  // rather than hard-coding SGD.
  templateFamilyOfFourSubtitle: (currency: string) => `10 categories · ${currency}`,
  templateFiftyThirtyTwenty: "50 / 30 / 20",
  templateFiftyThirtyTwentySubtitle: "Needs · wants · savings",
  templateImportLastMonth: "Import last month",
  templateImportLastMonthSubtitle: (prevMonth: string) => `Copy ${prevMonth}'s caps`,

  // Shown in the (Task 14) modal the instant the 50/30/20 card is clicked,
  // while its prefill still has zero lines -- spec decision 6: that
  // template "has nothing to split" without an income figure on file.
  fiftyThirtyTwentyPrompt: "Enter your expected income and we'll split it 50/30/20",

  loadError: "Couldn't load your budget.",
} as const;
