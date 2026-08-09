// Copy for the Bills screen, in a plain .ts module for the same reason
// goalCopy.ts/budgetCopy.ts are -- eslint's react-refresh/only-export-components
// rule never has to think about a file that mixes components with other
// exports, and every user-facing string lives in exactly one place so a
// future tone sweep (goalCopy.ts's own header comment on why that mattered)
// has one file to open, not three.
//
// The date helpers below are exported alongside the copy, not hidden inside
// BillRow.tsx/BillStatCards.tsx/BillsPage.tsx separately, because all three
// need at least one of them -- goalCopy.ts's own monthNameOnly stays private
// to that file because nothing else needs it, but three call sites needing
// the identical parsing is past the point duplication earns its keep.

// "2026-07-24" parsed as LOCAL calendar components, never `new Date(dateStr)`
// directly -- that reads the string as UTC midnight, which a household west
// of UTC sees as the *previous* evening (the same mistake BudgetPage.tsx's
// monthLabel and GoalCard.tsx's targetMonthLabel both guard against for
// "YYYY-MM" strings; this is the "YYYY-MM-DD" sibling of that rule).
function parseDateOnly(dateOnly: string): Date {
  const [year, month, day] = dateOnly.split("-").map(Number);
  return new Date(year, month - 1, day);
}

// "2026-07-24" -> "Jul 24". The row date badge's combined label and the
// Next-due stat card's own value -- month-first, matching the design's own
// "Jul 24 · Tax GIRO".
export function monthDayLabel(dateOnly: string): string {
  return parseDateOnly(dateOnly).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

// "2026-07-24" -> "24 Jul" -- day-first, for the overdue sentences and the
// All caught up panel's "Next bill: X, 15 Sep" clause (both pinned verbatim
// by the task brief in this order). Composed by hand, not by trusting a
// locale option to flip toLocaleDateString's own month-then-day ordering --
// there is no `{ day: "numeric", month: "short" }` combination that reorders
// under "en-US".
export function dayMonthLabel(dateOnly: string): string {
  const date = parseDateOnly(dateOnly);
  return `${date.getDate()} ${date.toLocaleDateString("en-US", { month: "short" })}`;
}

// The row date badge's own two lines (BillRow.tsx) -- kept as two functions
// rather than splitting monthDayLabel's own string apart again, which would
// make the badge's shape depend on there being exactly one space in a
// formatted string instead of on the date itself.
export function monthAbbrev(dateOnly: string): string {
  return parseDateOnly(dateOnly).toLocaleDateString("en-US", { month: "short" }).toUpperCase();
}
export function dayNumber(dateOnly: string): string {
  return String(parseDateOnly(dateOnly).getDate());
}

// The Repeats dropdown's options. billSchemas.ts's own cadenceSchema stays
// private to that file (its header comment: Bill's inferred `cadence` field
// already carries the union everywhere a caller needs it, the same reason
// goalStatusSchema is never exported either) -- exporting it just for this
// list would widen that file's public surface for a value BillModal.tsx
// already gets, typed, from CreateBillBody["cadence"] (useBills.ts). Declared
// here rather than inline in BillModal.tsx because the labels are
// user-facing copy, and this file's own job is being the one place every
// user-facing Bills string lives.
export const CADENCE_OPTIONS = [
  { value: "one_off", label: "One-off" },
  { value: "monthly", label: "Monthly" },
  { value: "quarterly", label: "Quarterly" },
  { value: "yearly", label: "Yearly" },
] as const;

// Row-level cadence label for the Subscriptions panel (SubscriptionsCard.tsx)
// -- derived from CADENCE_OPTIONS above rather than a second, separately
// typed-out set of the same four strings, so a future rename of one only
// ever has one place to change.
export const CADENCE_LABELS: Record<string, string> = Object.fromEntries(
  CADENCE_OPTIONS.map((option) => [option.value, option.label]),
);

// Shared between isSubscriptionLabel below (the modal's own checkbox) and
// subscriptionsEmptyBody (the panel's own empty state) -- the empty state
// names this exact label so a household reading it can go straight to the
// control that makes the panel stop being empty, and a future rename of the
// checkbox's own words cannot silently leave that sentence quoting stale text.
const IS_SUBSCRIPTION_LABEL = "Counts as a subscription";

export const BILL_COPY = {
  title: "Bills & subscriptions",
  // M = 0 hides the line rather than rendering "0 of 0" -- the formulas
  // table's own rule, restated from GOAL_COPY.onTrack's identical guard.
  onAutopay: (autopayCount: number, billCount: number) => `${autopayCount} of ${billCount} on autopay`,
  addBill: "+ Add bill",

  archivedToggle: "Show archived",
  // AccountsPanel.tsx's own `noneArchived` shape: shown when the toggle is
  // on and the union came back with nothing archived in it.
  archivedEmpty: "No archived bills.",
  archivedSection: "Archived",
  archivedMarker: "(archived)",
  // The counterpart to restore below, and the reason a dedicated Archived
  // section exists at all: Goals shipped "Show archived" and Restore with no
  // way to ever reach the archived state (docs/LEARNING.md pattern 15). This
  // task's own three archive/restore tests are what closes the same gap here.
  archive: "Archive",
  restore: "Restore",

  emptyHeadline: "No bills yet",
  emptyBody:
    "Add the household's fixed costs — rent, insurance, subscriptions — and Hearth will track what's due and what's already been paid.",

  dueSoon: "Due soon",
  // Shown in place of the list when nothing is due soon, so the heading
  // never sits blank above nothing -- the task brief's own words: "reads as
  // a loading bug rather than an achievement," the exact failure the interim
  // Overview's blank limited-member page already produced once.
  dueSoonEmpty: "Nothing due in the next 30 days.",
  later: "Later",
  paidThisMonth: "Paid this month",

  statDueThisMonth: "Due this month",
  statPaidSoFar: "Paid so far",
  statNextDue: "Next due",
  // The zero-state's own copy (state 2's contract line: "stat cards render
  // zeros with their own copy," not bare "S$0.00") -- shown as a subtitle
  // beside the figure whenever it actually is zero, on either card
  // independently, since a household can easily have nothing due this month
  // while still having paid something (or the reverse).
  // Leading "· " matches nextDueBillNameClause's own connector, so a zero
  // card's subtitle reads consistently with the Next-due card's "· Tax
  // GIRO" alongside it.
  dueThisMonthZero: "· Nothing due this month",
  paidSoFarZero: "· Nothing paid yet",
  // Replaces the date value outright on an overdue next-due bill -- "names
  // it as overdue rather than printing a past date as though upcoming" (the
  // task brief's own words for state 5).
  nextDueOverdueValue: "Overdue",
  nextDueBillNameClause: (billName: string) => `· ${billName}`,

  paidLabel: "✓ Paid",
  autopayPill: "Autopay",
  overduePill: "Overdue",
  // settled renders where a date would go for a paid one-off with no next
  // occurrence -- billDTO's own comment on why it is never dropped from the
  // page even though it satisfies neither Due soon's nor Later's own
  // definition.
  settled: "Settled",

  // The due-soon/later row's subtitle, e.g. "Tax · autopay · DBS". One
  // function for both flags rather than four separate copy strings, since
  // the sentence's shape (three clauses joined by " · ") is itself part of
  // what a tone sweep would want to change in one place.
  rowSubtitle: (categoryName: string, autopay: boolean, accountName: string) =>
    `${categoryName} · ${autopay ? "autopay" : "manual"} · ${accountName}`,
  // A payment (billPaymentSchema) carries no categoryName or accountName --
  // it is not an oversight in the design's own mockup ("Education · manual"
  // has no account either), it is what the DTO actually has.
  paymentSubtitle: (autopay: boolean) => (autopay ? "autopay" : "manual"),

  // The one moment autopay earns its place (spec decision 3): the same fact
  // — a bill is overdue — reads as two different calls to action depending
  // on who was supposed to act.
  overdueAutopay: (dateLabel: string) => `Should have gone out on ${dateLabel} — confirm it did`,
  overdueManual: (dateLabel: string) => `Overdue since ${dateLabel}`,

  allCaughtUpHeadline: "All caught up",
  // The leading "— " and the lowercase "everything" both match the task
  // brief's own quoted sentence verbatim ("All caught up — everything due
  // in August is paid. Next bill: school fees, 15 Sep.") -- rendered as a
  // second element below the bold headline (the design's own two-part "All
  // bills covered" panel shape), with the dash carried into this string so
  // the two still read as one sentence split across a line break.
  //
  // nextBillClause is composed separately and passed in already-built
  // (rather than this function taking the raw billName/date pair itself) so
  // BillsPage.tsx can omit it entirely when summary.nextDue is null --
  // "None -> omitted, never rendered as a zero," the formulas table's own
  // rule for Next due, restated here for the same figure.
  allCaughtUpBody: (monthName: string, nextBillClause: string | null) =>
    `— everything due in ${monthName} is paid.${nextBillClause ? ` ${nextBillClause}` : ""}`,
  nextBillClause: (billName: string, dateLabel: string) => `Next bill: ${billName}, ${dateLabel}.`,

  // Same shape as BUDGET_COPY.excludedNoRate/GOAL_COPY.excludedNoRate --
  // one wording for "N of these could not be converted," reused across the
  // three money screens that each carry their own excludedNoRate count.
  excludedNoRate: (count: number) =>
    `${count} ${count === 1 ? "bill is" : "bills are"} not counted: no exchange rate.`,

  loadError: "Couldn't load your bills.",

  // BillModal (Task 13) -- add and edit, one modal, the TransactionModal.tsx
  // pattern (the task brief's own words). The two titles vary by mode, unlike
  // GoalModal.tsx's/AccountModal.tsx's one generic title for both -- the
  // design draws "Add a bill" as this screen's own words, and there is no
  // reason to discard them just because Edit needs a second string.
  addBillModalTitle: "Add a bill",
  editBillModalTitle: "Edit bill",
  // Shown in place of the form while BillModal's own useAccounts() is still
  // settling (BillModal.tsx's own header comment on why that one query
  // gates the split). Ten other files in this codebase hardcode this exact
  // string inline rather than through their own copy file -- unremarkable by
  // house convention, kept here anyway because billCopy.ts's own rule for
  // this file is the stricter "every user-facing string lives here."
  loading: "Loading…",
  // The empty state's own call to action -- distinct copy from the header's
  // "+ Add bill" above (addBill) on purpose: both render on screen together
  // whenever a household has zero live bills, and identical text on two
  // buttons is two elements answering to the same accessible name.
  createFirstBill: "Add your first bill",

  billNameLabel: "Bill name",
  billNamePlaceholder: "e.g. StarHub, HDB loan",
  amountLabel: "Amount",
  repeatsLabel: "Repeats",
  nextDueLabel: "Next due",
  categoryLabel: "Category",
  payFromLabel: "Pay from",
  paidByLabel: "Paid by",
  noCategoryOption: "No category",
  unassignedPayer: "Unassigned",
  chooseADueDate: "Choose a due date.",
  amountMustBePositive: "Enter an amount greater than zero.",
  // Every other modal in this codebase (GoalModal.tsx, BudgetModal.tsx,
  // AccountModal.tsx) repeats this exact fallback inline rather than through
  // its own copy file -- kept here instead, not to break with them, but
  // because this file's own rule (every user-facing string lives in
  // billCopy.ts) is the stricter one this task was asked to hold to.
  genericSaveError: "Something went wrong. Please try again.",

  onAutopayLabel: "On autopay",
  // The rewritten toggle copy (spec decision 3), not the design's "Mark as
  // automatically paid — otherwise you'll get a reminder". That sentence
  // promises two things this product does not do: nothing pays itself
  // (autopay is a display flag only -- every bill, autopay or manual, is
  // marked paid by a person through the same MarkPaid code path), and the
  // bill-due reminder has never sent anything (the Settings toggle for it has
  // existed and done nothing since slice 1). This sentence promises only what
  // MarkPaid (Task 14) actually does.
  onAutopayHelp: "The bank pays this one — we'll still ask you to confirm it went out.",

  isSubscriptionLabel: IS_SUBSCRIPTION_LABEL,
  // Spec decision 4: a subscription is a bill with a flag the household set,
  // never one the code inferred from its category or cadence -- this says
  // what ticking the box does, not a claim about what the bill "is".
  isSubscriptionHelp: "Included in the household's subscription totals.",

  cancelAction: "Cancel",
  addBillSubmit: "Add bill",
  saveBillSubmit: "Save",

  // The live-name-collision half of writeBillWriteError's own BILL_NAME_TAKEN
  // handling -- MapDomainError's generic message never carries the attempted
  // name, so this modal composes the same sentence GOAL_COPY's own fallback
  // does for goals, restated for a bill.
  billNameTaken: (name: string) => `"${name}" is already the name of a bill in this household.`,

  // MarkPaidModal (Task 14). Reachable from every LIVE bill row -- Due soon,
  // Later, overdue and settled alike -- never from an archived one: writeMarkPaidError's
  // own BILL_ARCHIVED message already reads "Restore it before marking a
  // payment," and the archived section's own affordance is Restore, not a
  // second way to pay (BillRow.tsx's own comment on this choice). Settled is
  // deliberately NOT excluded client-side, unlike archived: a settled one-off
  // is exactly the shape BILL_SETTLED exists to catch -- a household that
  // forgot it already paid a one-off, or two members marking the same
  // occurrence within moments of each other -- and hiding the control would
  // make that refusal untestable by a real click rather than making it
  // impossible to reach.
  markPaidModalTitle: "Mark as paid",
  markPaidTrigger: "Mark paid",
  markPaidAriaLabel: (billName: string) => `Mark ${billName} paid`,
  paidOnLabel: "Paid on",
  markPaidSubmit: "Mark paid",
  // Spec decision 1's own accepted cost, stated at the point of clicking: a
  // household that marks a bill paid *and* separately hand-enters the same
  // expense double-counts it. Naming the ledger description (the bill's own
  // name) is what makes that duplicate recognisable later, on the
  // Transactions screen -- the task brief's own reason this sentence exists
  // at all.
  markPaidWritesExpense: (billName: string) =>
    `This writes an expense to the ledger, described as "${billName}". If you've already entered this payment yourself, marking it paid here will double-count it.`,

  // Undo (Task 14), the GoalContributionsPanel.tsx pattern: an in-page
  // confirmation, never window.confirm. Shown on every row in "Paid this
  // month" regardless of position -- the server's own rule is per-bill
  // (only that bill's own most recent payment can be undone), and
  // paidThisMonth mixes payments from different bills, each already its own
  // latest. This screen has no way to compute which rows would succeed
  // without asking the server, so every row offers Undo and
  // BILL_PAYMENT_NOT_LATEST's own message (surfaced verbatim, naming the
  // due date that IS undoable) is what tells a household when it guessed
  // wrong -- the honest design, not a client-side guess that is wrong
  // whenever a bill's own most recent payment is not the newest row on
  // screen.
  undoTrigger: "Undo",
  undoAriaLabel: (billName: string) => `Undo ${billName} payment`,
  undoConfirmBody: "This removes the expense it wrote to the ledger.",
  undoConfirmAction: "Undo payment",

  // SubscriptionsCard (Task 15), beside the lists on BillsPage's own
  // two-column grid. subscriptionsMonthlyMinor/subscriptionsAnnualMinor
  // (BillsSummary) are the server's own rollup -- bill.go's own comment:
  // integer-first, one division, at the very end -- so every figure this
  // card shows is formatted here, never re-summed from the bills array.
  subscriptionsTitle: "Subscriptions",
  // One composed string, not two sibling elements read as one line only by
  // CSS spacing -- the design draws "Subscriptions" and "S$70.90/mo" as
  // separate divs, but a screen reader (and a test asserting text content)
  // needs the "· " connector to actually be text.
  subscriptionsHeading: (monthlyLabel: string) => `Subscriptions · ${monthlyLabel}/mo`,
  subscriptionsAnnualLine: (annualLabel: string) => `${annualLabel}/year`,
  // The task brief's own point 2: a row shows what is actually charged (a
  // quarterly bill's own S$120), while the heading and this line show the
  // monthly-equivalent totals the server already normalised -- without this
  // sentence a household has no way to tell the two kinds of figure apart on
  // the same card. Says nothing about when the totals were last checked --
  // the design's own "last reviewed Mar 2026" names a date nothing in this
  // product can set (point 3), so it is not carried into this rewrite.
  subscriptionsEquivalentNote: "Totals above are monthly equivalents — each row shows what's actually charged.",
  // The empty state (point 4): explains how a bill becomes a subscription --
  // the checkbox in the Add/Edit-bill modal -- rather than leaving a
  // household with bills and a blank panel and no way to know what it is
  // for. Only true when NOTHING is ticked -- SubscriptionsCard picks between
  // this and subscriptionsEmptyExcludedBody below depending on which is
  // actually the case, so this one never has to cover the ticked-but-
  // excluded state by accident.
  subscriptionsEmptyBody: `No bills are marked as subscriptions yet. Tick "${IS_SUBSCRIPTION_LABEL}" when adding or editing a bill to see it listed here.`,
  // A second empty state a review round found this panel's first cut did
  // not have: a household CAN tick "Counts as a subscription" on a bill and
  // still see this same blank panel, because a one-off or an archived bill
  // is excluded regardless of the flag (SubscriptionsCard.tsx's own filter
  // comment). subscriptionsEmptyBody's "not marked yet" would be false in
  // that state -- the household marked one, and Hearth is not showing it --
  // so this names the actual reason instead of repeating a sentence that
  // contradicts what the household just did.
  subscriptionsEmptyExcludedBody:
    "A bill marked as a subscription isn't always counted here: a one-off isn't a recurring cost, and an archived bill isn't a live one.",
} as const;
