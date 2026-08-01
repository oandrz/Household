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
  // "so far" is dropped for a past month -- daysLeft reaching 0 is the
  // server's own signal for that (spec's formulas table: days left is 0 for
  // a past month), and "Spent so far" for a month that finished weeks ago
  // reads as if the screen doesn't know it's over. A future month still has
  // daysLeft > 0, so it keeps "so far" -- spec-consistent, since a month
  // that hasn't started yet hasn't finished either.
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

  // BudgetModal.tsx (Task 14) copy below.
  expectedIncome: "Expected income",
  allocated: "Allocated",
  leftToAllocate: "Left to allocate",
  categoryName: "Category name",
  cap: "Cap",
  removeRow: "Remove",
  archiveRow: "Archive",
  addACategory: "+ Add a category",
  chooseACategory: "Choose a category…",
  newCategoryOption: "New category…",
  newCategoryName: "New category name",
  addCategory: "Add",
  saveBudget: "Save budget",
  cancel: "Cancel",
  // The suggestion list built from a template's `missing` names (spec
  // decision 6, budgetTemplates.ts's own comment) -- a name the template
  // named but the household has no live category for yet.
  suggestedByTemplate: "Suggested by this template",
  // Shown on a row whose "Add" resolved to an ARCHIVED category rather than
  // a brand-new one (the archived-name gotcha the Task 13 review flagged:
  // creating a category with a name an archived row already holds 409s on
  // categories_household_id_name_key). Saving restores it instead of
  // creating a duplicate name the database would refuse anyway.
  willRestore: "Archived -- saving will restore it instead of creating a duplicate.",
  // 409 CATEGORY_NAME_TAKEN's server message is generic ("A category with
  // that name already exists.", no name in `details`) -- named here from
  // the name the modal itself just attempted, which it already knows
  // client-side, rather than waiting on a server response that never
  // carries it.
  categoryNameTaken: (name: string) => `"${name}" is already a category name in this household.`,

  // BudgetHistoryModal.tsx (Task 15) copy below.
  historyModalTitle: "Budget history",
  historyModalSubtitle: (months: number, currency: string) =>
    `Spent vs. budgeted, last ${months} months · ${currency}`,
  historyAvgSpend: "Avg monthly spend",
  historyAvgSaved: "Avg saved / month",
  historyMonthsUnderBudget: "Months under budget",
  // "N of D" -- D is closed *budgeted* months only (see
  // BudgetHistoryModal.tsx's own comment on why the current month and any
  // month with a zero-cap budget are excluded from both this and the two avg
  // cards). "0 of 0" is a real, renderable value for a brand-new household
  // with no closed months yet, not an error state.
  historyMonthsUnderBudgetValue: (under: number, of: number) => `${under} of ${of}`,
  historySoFar: "so far",
  // The design's own footnote (Household Dashboard.dc.html's Budget history
  // modal), naming the current month the same way BudgetPage.tsx's
  // `monthNameOnly` does -- month only, no year.
  historyFootnote: (monthName: string) => `* ${monthName} is still in progress. Click any month to open its full breakdown.`,
  // Shown instead of the summary cards and table when `months` is empty --
  // a brand-new household with no history yet, not a fetch failure (the page
  // already has its own `loadError` for that). Matches emptyBody's plain,
  // explanatory tone rather than a bare "Nothing here."
  historyEmpty: "No budget history yet. Once a month closes, it shows up here.",
  noValue: "—",

  // Task 15's own copy: BudgetRolloverCard.tsx, the manual half of "Roll
  // unspent into savings" (see onPaceToSave's own comment above on why the
  // design's automatic version does not ship). Every string here is worded
  // as a one-time, clicked action -- "unspent," "Move," "moved" -- never
  // the design's own present-tense "rolls into," so BudgetPage.test.tsx's
  // own guard has nothing to falsely trip on once this real copy is on
  // screen.
  rolloverOffer: (amount: string, monthName: string) => `${amount} unspent in ${monthName}`,
  moveIntoGoal: "Move it into a goal",
  chooseAGoal: "Choose a goal…",
  // Spec decision 11: budgets carry no currency column of their own and are
  // implicitly the household's primary currency, while a goal carries an
  // explicit one -- converting inside a rollover would store a rate nobody
  // can audit, so only a primary-currency goal can receive one. A goal in
  // another currency is still listed in the picker, disabled, with this
  // reason right on its own option -- never silently filtered away.
  rolloverIneligibleOption: (goalName: string, goalCurrency: string, primaryCurrency: string) =>
    `${goalName} (${goalCurrency} — only ${primaryCurrency} goals can receive a rollover)`,
  rolloverNoGoalsYet: "You don't have any savings goals yet.",
  rolloverNoPrimaryCurrencyGoal: (currency: string) =>
    `None of your goals are in ${currency}, the only currency a rollover can move money into.`,
  rolloverLoadError: "Couldn't load your goals.",
  // The destination sentence, once the month carries the stamp for good.
  // Past tense, naming one action already taken -- never "rolls into,"
  // which reads as automatic and recurring.
  rolledOverDone: (amount: string, goalName: string) => `${amount} moved into ${goalName}.`,
  // The same sentence without a name, for the edge case where the goal a
  // past rollover named can no longer be found in the live goals list this
  // render fetched (GET /goals, no include_archived -- BudgetRolloverCard.tsx's
  // own header comment on why that is the cheaper, still-honest answer
  // rather than a second archived-inclusive fetch just to resolve a name).
  rolledOverDoneUnknown: (amount: string) => `${amount} moved into a goal.`,
} as const;
