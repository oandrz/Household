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
import type { GoalContribution } from "./goalSchemas";

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
  // The counterpart to restore below, and the reason it is here at all:
  // Task 11 shipped "Show archived" and Restore with no way to ever reach
  // the archived state (useGoals' archiveGoal had a test and no caller).
  // Found by the Task 18 browser walk at criterion 12 -- see docs/LEARNING.md.
  archive: "Archive",
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

  // Task 13's own copy: the "Add contribution" control on a live card, and
  // the panel it opens.
  addContribution: "Add contribution",
  contributionsTitle: (goalName: string) => `${goalName} — contributions`,
  noContributionsYet: "No contributions yet.",
  // amount_minor <> 0 is the CHECK constraint the API enforces (spec
  // decision: "a mistyped contribution is corrected by a negative row, not
  // refused outright"), so this refuses exactly zero -- never `<= 0`, which
  // would also wrongly block a legitimate correction.
  zeroAmountError: "Enter an amount other than zero.",
  deleteContributionTrigger: "Delete",
  deleteContributionConfirmBody: "Delete this contribution? This can't be undone.",
  // Distinct from deleteContributionTrigger above on purpose -- the two are
  // never on screen at once (TransactionModal.tsx's own confirmingDelete
  // toggle: the trigger hides the moment this pair appears), but a distinct
  // word for the destructive confirm still reads more deliberately than
  // reusing "Delete" for both the opening click and the one that actually
  // commits it.
  deleteContributionConfirmAction: "Yes, delete",
  deleteContributionCancelAction: "Cancel",
  // manualContributionFallback covers a manual row whose note is blank --
  // the field is optional (addContributionRequest's own Note has no
  // non-empty check), so "shows its own note" would otherwise render an
  // empty line in the list, which reads as a rendering bug rather than "no
  // note."
  manualContributionFallback: "Manual contribution",
  startingBalanceLabel: "Starting balance",
  rolloverLabel: (monthName: string) => `From ${monthName}'s unspent budget`,
  // rolloverUnknownMonth is the fallback for a budget_rollover row whose
  // sourceBudgetMonth is null -- an invariant the backend never actually
  // produces (domain.GoalContribution's own comment: the column is set
  // exactly when Source is ContributionBudgetRollover), but the wire type is
  // nullable regardless (goalContributionSchema's own sourceBudgetMonth is
  // `.nullable()`, not narrowed to the source), so this is the fail-closed
  // reading rather than a crash or a literal "From null's unspent budget."
  rolloverUnknownMonth: "From an earlier month's unspent budget",

  // Task 14's own copy: the Monthly contributions card below the grid. Two
  // figures render side by side rather than folded into one (spec decision
  // 7), so the labels are deliberately parallel and neither says "total"
  // alone -- plannedMonthlyHeading scopes the bar+legend above it,
  // plannedMonthlyTotalLabel and actualThisMonthLabel scope the two footer
  // figures, and nothing here could be read as the other.
  contributionsCardTitle: "Monthly contributions",
  plannedMonthlyHeading: "Planned monthly",
  plannedMonthlyTotalLabel: "Planned monthly total",
  actualThisMonthLabel: "Actual this month",
  // actualNone fires only when something was planned to compare against --
  // a household with nothing planned and nothing logged has no divergence
  // to report either way (MonthlyContributionsCard.tsx's own guard). Worded
  // as an observation ("nothing yet") rather than reusing actualShort's
  // verdict phrasing, even though "nothing logged" is arithmetically just
  // the largest possible shortfall -- the task brief's own instruction that
  // a zero actual "says so in words rather than hiding the figure".
  actualNone: "Nothing logged yet this month.",
  actualShort: (amount: string) => `${amount} short of plan this month.`,
  actualOver: (amount: string) => `${amount} more than planned this month.`,

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

// "2026-07" -> "July" -- month name only, matching the design's own example
// verbatim ("From July's unspent budget," no year). Mirrors GoalCard.tsx's
// own targetMonthLabel and BudgetHistoryModal.tsx's own monthNameOnly:
// anchored on day 2, not day 1, for the identical UTC-offset reason both of
// those already document. Kept private to this file rather than imported
// from either sibling -- a small duplicated four-line function is the
// established trade-off here (TransactionModal.tsx's own today() makes the
// same call), not a shared date-formatting module.
function monthNameOnly(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "long" });
}

// contributionSourceLabel is GoalContributionsPanel.tsx's own per-row label,
// composed here rather than in that component because it is copy, not
// layout -- the same separation budgetCopy.ts/transactionCopy.ts already
// keep from their own pages. The server deliberately stores no copy for a
// rollover row (deviation 3, this repo's plan doc: "user-facing copy belongs
// in the frontend... not composed in a Go handler"), so this is the one
// place `source` + `sourceBudgetMonth` become a sentence.
//
// A plain function, not a GOAL_COPY property, because it switches on a real
// type rather than just formatting arguments -- the CLAUDE.md house rule
// ("a switch over a type that arrives from a database column or a request
// needs a default that refuses") applies to `source` here even though
// goalContributionSchema's z.enum has already validated it by the time this
// runs; the explicit `default` is the same belt-and-suspenders every other
// source-typed switch in this codebase keeps.
//
// The refusal renders less, the same shape GoalCard.tsx's own STATUS_PILL
// and its dateless-goal branch already take (no pill, no date clause, never
// a guess), rather than throwing: a throw here would happen mid-render,
// inside a modal with no error boundary above it, taking the whole panel
// down over a single row that Zod's own z.enum would already have caught
// one layer up (an unknown source fails the whole GET's schema parse first,
// which useGoalContributions surfaces as `error` -- this function's default
// case is the belt-and-suspenders for a future looser schema or a cast, not
// the primary defence).
export function contributionSourceLabel(
  source: GoalContribution["source"],
  sourceBudgetMonth: string | null,
  note: string,
): string {
  switch (source) {
    case "manual":
      return note.trim() === "" ? GOAL_COPY.manualContributionFallback : note;
    case "starting_balance":
      return GOAL_COPY.startingBalanceLabel;
    case "budget_rollover":
      // rolloverUnknownMonth is the whole sentence already, not a month
      // fragment -- fed straight through rather than into rolloverLabel,
      // which would otherwise double up into "From From an earlier
      // month's...".
      return sourceBudgetMonth
        ? GOAL_COPY.rolloverLabel(monthNameOnly(sourceBudgetMonth))
        : GOAL_COPY.rolloverUnknownMonth;
    default: {
      const refused: never = source;
      void refused;
      return GOAL_COPY.manualContributionFallback;
    }
  }
}
