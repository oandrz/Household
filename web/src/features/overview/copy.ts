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

  // GoalsCard.tsx. Mirrors GOAL_COPY's own onTrack/withNoDate pair
  // (features/money/goalCopy.ts) rather than importing them -- a small
  // duplicated pair of one-liners is the established trade-off here
  // (goalCopy.ts's own monthNameOnly comment makes the identical call), not
  // coupling Overview's copy module to Money's for four lines. datedCount
  // === 0 hides goalsOnTrack's figure entirely (the design spec's own "X of
  // Y" rule) rather than this function ever being asked to render "0 of 0".
  goalsHeading: "Goals on track",
  goalsOnTrack: (onTrackCount: number, datedCount: number) => `${onTrackCount} of ${datedCount}`,
  // monthLabel is null exactly when the next goal (which the design's own
  // spec table guarantees is always dated when it exists at all) somehow
  // isn't -- an invariant this function stays fail-closed against rather
  // than trusting, the same reason goalSchemas.ts's own nextGoalSchema
  // leaves targetMonth nullable instead of narrowing it.
  goalsNext: (name: string, monthLabel: string | null) =>
    monthLabel ? `next: ${name} · ${monthLabel}` : `next: ${name}`,
  goalsWithNoDate: (count: number) => `${count} with no date`,
  // The household has no live goals at all yet -- not the same state as
  // datedCount: 0, which still has goals, just none with a date. Mirrors
  // budgetNone/budgetSetUp above: a way in, not a blank card.
  goalsNone: "No goals yet",
  goalsCreate: "Create a goal",

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
  // No precondition, unlike Transaction above -- the account dependency
  // that shape follows disappeared for goals with decision 6 (contributions
  // move no real money), so this entry is never disabled.
  quickAddGoal: "Savings goal",
} as const;

