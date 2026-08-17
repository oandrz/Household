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

  // NextBillCard.tsx. amountLabel/dateLabel are pre-formatted by the caller
  // (formatMoney/monthDayLabel), the same split BudgetCard's own budgetOf
  // follows -- this file's job is which words surround a figure, never the
  // figure's own formatting.
  nextBillHeading: "Next bill",
  nextBillClause: (billName: string, dateLabel: string) => `${billName} · ${dateLabel}`,
  // Overdue replaces the date half of the clause outright rather than
  // sitting beside it -- BillStatCards.tsx's own nextDueOverdueValue rule
  // for the identical figure, restated here: "names it as overdue rather
  // than printing a past date as though upcoming."
  nextBillOverdueClause: (billName: string) => `${billName} · Overdue`,
  // The household has never added a live bill at all -- mirrors
  // budgetNone/goalsNone's own "nothing set up yet" shape below.
  nextBillNone: "No bills yet",
  nextBillAdd: "Add a bill",
  // Distinct from nextBillNone: summary.billCount > 0 here, so this
  // household has live bills, just none with an upcoming due date -- every
  // one is a settled one-off (paid, no next occurrence). The GoalsCard.tsx
  // achieved-goal fix is the precedent for why this needs its own line
  // rather than falling into the "none yet" branch above: a household with
  // real bills is not bill-less just because none is due next.
  nextBillCaughtUp: "Nothing due right now",

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
  quickAddBill: "Bill",
  // Shared by Transaction and Bill: a transaction attaches to an account,
  // and a bill needs a pay-from account, so both entries open a modal whose
  // account dropdown would be empty with none -- the dead end
  // TransactionsPage's own comment refuses. One string covers both rather
  // than two copies of the same sentence, the same reasoning
  // CADENCE_LABELS (billCopy.ts) gives for deriving instead of duplicating.
  quickAddNeedsAccount: "Add an account first",
  // No precondition, unlike Transaction/Bill above -- the account
  // dependency that shape follows disappeared for goals with decision 6
  // (contributions move no real money), so this entry is never disabled.
  quickAddGoal: "Savings goal",

  // NextRetroCard.tsx. Short strings duplicated locally rather than
  // importing RETRO_COPY (features/marriage/retroCopy.ts) -- the same
  // GoalsCard.tsx/GOAL_COPY trade-off stated above: a few one-liners here,
  // not a coupling from Overview's copy module to Marriage's.
  nextRetroHeading: "Next retro",
  // "August retro" -- monthName comes from marriage/retroCopy.ts's own
  // monthNameOnly, imported directly (a pure formatting function, not a
  // copy string, the same distinction NextBillCard.tsx draws by importing
  // money/billCopy.ts's monthDayLabel directly instead of duplicating it).
  nextRetroTitle: (monthName: string) => `${monthName} retro`,
  nextRetroInProgress: "In progress",
  // actionCount is retroSummarySchema's own field -- the retro's TOTAL
  // action count, not filtered to open ones (retro.sql's own action_count:
  // "SELECT count(*) FROM retro_actions", no done_at filter). This card
  // cannot show which of them are still open without a second request
  // (GET /retros/{month}, useRetro(month)) -- see NextRetroCard.tsx's own
  // header comment for why it does not make one. Omitted entirely at zero,
  // the same "never a bare count of nothing" rule nextBillCaughtUp/
  // goalsNone above already follow.
  nextRetroActions: (count: number) => `${count} action${count === 1 ? "" : "s"}`,
  nextRetroNone: "No retro yet this month",
  // Mirrors RETRO_COPY.startRetro's own "Start X retro" wording -- same
  // local-duplicate trade-off as nextRetroInProgress above.
  nextRetroStart: (monthName: string) => `Start ${monthName} retro`,
  // startMonth is nullable (both candidate months already have a retro is
  // the ordinary reason, but retrosResponseSchema also allows it to be null
  // with no current-month retro either -- a shape this schema permits even
  // though RetroService.List should never actually produce it). This is
  // the fail-closed fallback for that state: a plain way in, never a
  // "Start null retro" string built from a month that was not there.
  nextRetroGo: "Go to Retros",
} as const;

