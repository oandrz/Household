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

  // RetroDetail.tsx's own load-error copy -- distinct from loadError below,
  // which is GET /retros' (the history list's) own failure copy. A reader
  // selecting a month whose detail 500s must not be told "your retros"
  // failed to load when the list beside it is sitting there, loaded fine.
  detailLoadError: "Couldn't load this retro.",

  wentWellHeading: "What went well",
  wasHardHeading: "What was hard",
  notesHeading: "Notes",

  // "Actions for July" over a June retro's own action list -- the design's
  // own heading (dc.html): actions decided in a retro are carried out the
  // month AFTER it, not the retro's own month.
  actionsHeading: (monthName: string) => `Actions for ${monthName}`,

  // An action's own provenance line, rendered only when RetroAction.carriedFrom
  // is non-"" (retroActionSchema's own comment). See previousMonthName below
  // for why this is always resolvable from the retro's own month rather than
  // needing the carried-from id resolved.
  carriedFrom: (monthName: string) => `Carried from ${monthName}`,

  moodLabel: (mood: number) => `mood ${mood}/5`,

  // RetroDetail.tsx's own per-row tick failure -- a network error or a
  // household mid-offline moment must not look like the click did nothing.
  tickError: "Couldn't update that action. Try again.",

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

  // RetroModal.tsx (Task 13) below. Distinct string from privacyBadge above
  // -- the page header and the modal show genuinely different copy at the
  // design's own two spots (dc.html: "🔒 Private — parents only" on the
  // header badge, "🔒 Private — just the two of you" under the modal's
  // title), the same "two real shapes, not one reused" reasoning
  // monthNameOnly/monthYearLabel already give for this file's month helpers.
  modalPrivacyBadge: "🔒 Private — just the two of you",

  // The mood radio group's own legend (dc.html's modal: "How was this
  // month, overall?"), and the five options themselves in the order the
  // mockup draws them, worst to best -- retroDTO.Mood/domain.ParseMood's own
  // 1..5 scale, so option N's value is literally N. `label` is each radio's
  // own accessible name (aria-label): the emoji alone means nothing to a
  // screen reader, and the tile's visible glyph is the emoji, not this text.
  moodQuestion: "How was this month, overall?",
  moodOptions: [
    { value: 1, emoji: "😞", label: "Terrible" },
    { value: 2, emoji: "😕", label: "Not great" },
    { value: 3, emoji: "😐", label: "Okay" },
    { value: 4, emoji: "🙂", label: "Good" },
    { value: 5, emoji: "😄", label: "Great" },
  ] as const,

  // The two textareas' own placeholders, copied verbatim from dc.html's
  // modal ("One line per win… (each of you adds their own)" /
  // "Be honest, be kind…") -- wentWellHeading/wasHardHeading above are
  // reused as-is for these fields' visible labels, since RetroDetail.tsx
  // already names the same two concepts with the same two words.
  wentWellPlaceholder: "One line per win… (each of you adds their own)",
  wasHardPlaceholder: "Be honest, be kind…",
  // Notes has no placeholder in the mockup at all -- see RetroModal.tsx's
  // own header comment on why the field exists here despite that.
  notesPlaceholder: "Anything else worth remembering for next time…",

  saveDraft: "Save draft",
  finishRetro: "Finish retro",

  // The version-conflict banner (spec's "Error handling" table: a stale
  // PATCH answers 409 RETRO_CHANGED, and the screen "Never a merge, never a
  // red failure alert"). Names what happened and what to do, not a generic
  // failure -- and says explicitly that nothing typed was lost, which is the
  // entire reason this is refused rather than silently merged (decision 6).
  //
  // No "Reload" action here on purpose -- an earlier draft of this modal had
  // one, and a real browser walk against a real 409 showed exactly why it
  // was wrong: `retro.reload()` clears `useRetro`'s own `conflict` flag
  // (task-9-report.md's own contract, "clears on any successful refetch")
  // without touching this modal's local fields, so the banner would
  // disappear while the textareas still held the pre-conflict text: the very
  // next Save would then PATCH that stale text over whatever the partner
  // just wrote, with the new version attached and no error -- last write
  // wins, the exact shape decision 6 in the design spec rejects by name.
  // Closing this modal and reopening it is the only safe way to see the
  // partner's version, because that is a fresh mount: a new useRetro(month)
  // call, a fresh fetch, and a fresh seed of every field from it.
  conflictBanner:
    "Someone else saved this retro while you were editing. Nothing you typed here has been lost — but Save and Finish are turned off until you close this retro and reopen it to see their version.",

  modalSaveError: "Couldn't save this retro. Try again.",

  editRetro: "Edit",
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

// "2026-07" -> "June" -- the calendar month immediately before the given
// one. RetroDetail.tsx's own "Carried from June" label uses this rather than
// resolving RetroAction.carriedFrom itself: that field is the id of the
// SOURCE ACTION on the wire (RetroActionInput's own doc comment in
// ports.go, "the id of last month's action when this one was carried"), an
// opaque UUID with no month in it at all. Resolvable anyway, because the
// design's own decision 4 ("Only the immediately previous month is
// offered") guarantees a carried action's source is always this retro's own
// month minus one -- there is no other month it could ever be, TODAY:
// RetroService.Month (api/internal/usecase/retro.go) computes the ONLY
// candidate list a client can ever carry from as
// `OpenInMonth(month.AddDate(0, -1, 0))`, exactly one calendar month back,
// and no code path in this codebase yet calls addAction with a non-""
// carriedFrom at all (Task 13's Start/Edit modal, which will, does not
// exist yet).
//
// This is a real trust boundary, not a proven one: AddRetroAction's own SQL
// (retro_action_repo.go) checks only that carriedFrom resolves to an action
// belonging to a retro of THIS HOUSEHOLD, never that it belongs to the
// immediately previous month -- confirmed live by inserting a carried_from
// six months back in the dev database, which rendered a plausible but wrong
// "Carried from July" instead of the true source month. Nothing on the wire
// (retroActionDTO) names which month a carriedFrom id actually belongs to,
// so this frontend has no way to verify the assumption from data alone --
// only RetroService.Month's own offer-restriction keeps it true. Whoever
// builds the carry-over control (Task 13) MUST only ever pass a carriedFrom
// value taken from this retro's own `carryOver` list (RetroDetailResponse's
// sibling field, already exactly that one-month-back set) -- passing
// anything else breaks this label silently, not loudly.
export function previousMonthName(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 2, 2).toLocaleDateString("en-US", { month: "long" });
}

// "2026-06" -> "July" -- the design's own "Actions for July" heading over a
// June retro's action list (dc.html): what a retro decides gets done the
// month AFTER the retro, not during it.
export function nextMonthName(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum, 2).toLocaleDateString("en-US", { month: "long" });
}

// "2026-06-28T21:18:52+08:00" -> "Jun 28" -- the detail header's own
// completion date. Unlike every month-only helper above, this takes a real
// timestamp (retroDTO.CompletedAt is a *time.Time, task-7-report.md's own
// sample carries a full offset) straight into `new Date`, with none of
// monthNameOnly's UTC-midnight caution: that trap is specific to a bare
// "YYYY-MM-DD" being parsed as UTC midnight, which a full timestamp with its
// own offset never is.
export function completedDateLabel(completedAt: string): string {
  return new Date(completedAt).toLocaleDateString("en-US", { month: "short", day: "numeric" });
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
