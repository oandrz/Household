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

  isSubscriptionLabel: "Counts as a subscription",
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
} as const;
