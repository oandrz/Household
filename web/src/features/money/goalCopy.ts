// Copy for the Goals screen, in a plain .ts module for the same reason
// budgetCopy.ts/transactionCopy.ts are -- eslint's
// react-refresh/only-export-components rule never has to think about a file
// that mixes components with other exports.
//
// Task 10 shipped only a page title. Task 11 (GoalsPage, GoalCard) grows it
// into the real screen's copy: cards, empty state, archived view, the
// owner-only explanation. Two of the design's own lines are deliberately
// absent from this file, not merely unused -- "S$2,050 auto-saved on the 1st
// of each month" and "next transfer Aug 1" (the design's automation copy,
// spec's "Copy the design asserts and this feature does not ship" table).
// GoalsPage.test.tsx pins their absence the same way BudgetPage.test.tsx
// pins "rolls into"'s: a future copy-paste from the design has nowhere to
// land them back in.
export const GOAL_COPY = {
  title: "Goals",
  newGoal: "+ New goal",

  // The header subtitle, replacing the design's automation sentence in this
  // slot. onTrack/withNoDate are composed separately and joined only where
  // both are non-null (GoalsPage.tsx) -- the formulas table's own rule that
  // `datedCount === 0` hides the "X of Y on track" clause rather than
  // rendering "0 of 0", while a household with only dateless goals still
  // gets a subtitle instead of a blank one.
  onTrack: (onTrackCount: number, datedCount: number) => `${onTrackCount} of ${datedCount} on track`,
  withNoDate: (count: number) => `${count} with no date`,

  statusOnTrack: "On track",
  statusBehind: "Behind",
  statusAchieved: "Achieved",

  dateClause: (label: string) => `by ${label}`,
  perMonth: (amount: string) => `${amount}/mo`,
  // The Behind card's own arithmetic: "so 'Behind' is never a verdict
  // without its arithmetic" (task brief). Rendered only when
  // requiredMonthlyOk -- usecase/goal.go's own GoalView comment: false means
  // the card shows no "needs S$X/mo" line rather than a wrong one.
  needsPerMonth: (amount: string) => `Needs ${amount}/mo to catch up`,

  archivedMarker: "(archived)",
  archivedToggle: "Show archived",
  // AccountsPanel.tsx's own `noneArchived` shape: shown when the toggle is
  // on and the union came back with nothing archived in it, rather than
  // silently rendering the same live list with no explanation for why
  // nothing changed.
  archivedEmpty: "No archived goals.",
  restore: "Restore",

  emptyHeadline: "No savings goals yet",
  emptyBody:
    "Give a goal a target and Hearth will track progress from the contributions you log against it.",
  createFirstGoal: "Create your first goal",

  // Same shape as budgetCopy.ts's own excludedNoRate, which itself follows
  // transactionCopy.ts's TRANSACTIONS_COPY.excludedNoRate -- the ledger's
  // copy pattern the task brief names.
  excludedNoRate: (count: number) =>
    `${count} ${count === 1 ? "goal is" : "goals are"} not counted: no exchange rate.`,

  loadError: "Couldn't load your goals.",
  // GET /goals is money AND owner-gated (spec decision 10), so a limited
  // member holding money reaches this page and the request answers 403 --
  // not hypothetical: the interim Overview's one real defect was a page that
  // rendered nothing at all for exactly that member (docs/LEARNING.md
  // pattern 2). This explains why, rather than showing loadError above or a
  // blank page.
  ownerOnlyHeading: "Owner only",
  ownerOnlyBody: "Goals is visible to the household owner. Ask them if you'd like to see where things stand.",
} as const;
