// Copy for the Retros screen, in a plain .ts module for the same reason
// goalCopy.ts/budgetCopy.ts/billCopy.ts are -- eslint's
// react-refresh/only-export-components rule never has to think about a file
// that mixes components with other exports, and every user-facing string
// lives in exactly one place.
//
// Not in Task 10's brief file list (only RetrosPage.tsx/.test.tsx,
// router.tsx, Sidebar.tsx and its test are named) -- split out anyway,
// matching every one of its four siblings above, none of which keep their
// copy inline in the page component either.
import type { RetrosResponse } from "./retroSchemas";

export const RETRO_COPY = {
  title: "Marriage retros",
  subtitle: "Monthly check-in, just the two of us",

  // "12 done since Aug 2025" -- the design's own line (dc.html's is_retros
  // screen), rendered only when doneCount > 0 (RetrosPage.tsx's own guard);
  // never "0 done".
  doneSince: (doneCount: number, sinceLabel: string) => `${doneCount} done since ${sinceLabel}`,

  privacyBadge: "🔒 Private — parents only",

  // The design's own "Start July retro" -- month name only, no year (this
  // month is always within a year of today, so the year adds nothing a
  // reader needs). startMonth null means both candidate months already have
  // a retro -- RetrosPage.tsx renders no button at all in that case, so this
  // is never called with a null month.
  startRetro: (monthName: string) => `Start ${monthName} retro`,

  emptyHeadline: "No retros yet",
  emptyBody:
    "A monthly check-in for just the two of you -- what went well, what was hard, and what to try next.",
  // Distinct copy from the header's own startRetro() button above, both of
  // which render together the first time a household has zero retros and a
  // startable month -- BillsPage.tsx's own "+ Add bill"/"Create your first
  // bill" precedent: identical text on two buttons on the same screen is two
  // elements answering to one accessible name.
  createFirstRetro: "Start your first retro",

  historyTitle: "History",
  // The draft's own row label (RetroHistoryList.tsx's own RetroHistoryRow)
  // -- a retro with finished: false is still being written, so its row
  // shows this instead of the mood/action/quote line a finished retro's row
  // shows.
  draftInProgress: "In progress",

  // RetroHistoryList.tsx's own disclosure over a collapsed year -- the
  // design's own literal text ("Show 2025 (7 more) ↓"). Rendered from data
  // the page already holds (GET /retros is deliberately unbounded, per the
  // design doc's "List is deliberately unbounded" section), never a second
  // fetch.
  showOlderYear: (year: string, count: number) => `Show ${year} (${count} more) ↓`,

  moodChartTitle: "Mood over 12 months",
  // MoodChart.tsx's own empty state -- shown when every point in the
  // twelve-month series is a gap (nobody has finished a retro with a mood
  // yet). A chart with zero drawable points is not "an empty chart", it is
  // no chart at all; this replaces it rather than rendering an axis with
  // nothing on it.
  moodChartEmpty: "Not enough retros yet to chart a mood trend.",
  // The chart's own aria-label -- a line drawing is invisible to a screen
  // reader, so this names the range and how many of those months actually
  // carry a mood. Never claims a gap is "0" (moodPointDTO's own comment:
  // null is a gap, never 0) -- it says how many months were *tracked*,
  // not what an untracked month's value was.
  moodChartLabel: (fromLabel: string, toLabel: string, trackedCount: number, totalCount: number) =>
    `Mood from ${fromLabel} to ${toLabel}, ${trackedCount} of ${totalCount} months tracked`,
  // RetroHistoryList/RetroDetailPanel arrive in Tasks 11-12 -- this is the
  // placeholder shown in the mount point until a retro is selected there.
  detailPlaceholder: "Select a retro to see its detail.",

  loadError: "Couldn't load your retros.",
  // GET /retros is marriage-AND-owner gated (router.go's own comment on the
  // group: requireOwner is stacked even though a limited member can never
  // hold CapMarriage today -- domain.ErrLimitedCannotHoldMarriage already
  // refuses it one layer down, and the route does not lean on that alone).
  // This explains a 403 rather than showing loadError above or a blank page
  // -- the same branch GoalsPage.tsx/BillsPage.tsx/BudgetPage.tsx all carry
  // for their own owner-gated GETs (docs/LEARNING.md pattern 1's own entry
  // on the class of bug this branch exists to close).
  ownerOnlyHeading: "Owner only",
  ownerOnlyBody:
    "Retros are visible to the household owner. Ask them if you'd like to see where things stand.",

  startError: "Couldn't start a retro. Try again.",
} as const;

// "2026-07" -> "July" -- anchored on day 2, matching goalCopy.ts's own
// monthNameOnly and billCopy.ts's parseDateOnly for the identical reason:
// `new Date("2026-07-01")` parses as UTC midnight, which a household west of
// UTC reads as the previous evening once toLocaleDateString applies the
// local offset. Day 2 keeps every timezone this app runs in on the same
// calendar month.
export function monthNameOnly(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "long" });
}

// "2026-06" -> "June 2026" -- the history list's own row label (the design's
// own `dc.html` reads "June 2026", "May 2026", ...), distinct from
// monthNameOnly's bare "July" the header/empty-state Start buttons use.
// Genuinely a different shape, not the same helper reused twice: a history
// row has to stay unambiguous once a household has 13+ months of retros and
// the same month name recurs across years, where the Start button never
// shows more than one candidate month at a time and so never needs the year.
export function monthYearLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "long", year: "numeric" });
}

// "2025-08" -> "Aug 2025" -- the doneSince clause's own month format,
// short + year rather than monthNameOnly's long + no-year (the design's two
// month strings on this one screen are genuinely different shapes, not a
// single helper reused two ways).
export function sinceLabel(since: string): string {
  const [year, monthNum] = since.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "short", year: "numeric" });
}

// "2026-06" -> "Jun" -- MoodChart.tsx's own x-axis label, short with no
// year (the chart's own aria-label already states the range, so 320px of
// plot width doesn't have to repeat it on every third tick). A fourth month
// shape on this one screen, genuinely distinct from the three above: short
// like sinceLabel, but no year like monthNameOnly.
export function monthShortLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "short" });
}

// The header subtitle's second clause, composed here rather than inline in
// RetrosPage.tsx so the "never 0 done, and never render it off a `since`
// that came back null while doneCount is somehow positive" rule lives in one
// place. Fails closed: a `since` that is unexpectedly null while doneCount is
// somehow positive (a data shape this schema's own nullability allows even
// though the service should never produce it) renders nothing rather than a
// string built from a value that was not actually there.
export function doneSinceClause(data: Pick<RetrosResponse, "doneCount" | "since">): string | null {
  if (data.doneCount <= 0 || !data.since) return null;
  return RETRO_COPY.doneSince(data.doneCount, sinceLabel(data.since));
}
