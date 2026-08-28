// The Retros screen's history list -- one row per retro, grouped by year,
// with the design's "Show 2025 (7 more)" disclosure over older years
// (2026-08-16-hearth-retros-design.md, "The formulas, pinned" table's own
// "History row" entry, and the "List is deliberately unbounded" section:
// `GET /retros` returns every retro in one unpaged call, so expanding a
// collapsed year is a pure client-side reveal over data this component
// already holds, never a second fetch).
import { useState } from "react";
import { RETRO_COPY, monthYearLabel } from "./retroCopy";
import type { RetroSummary } from "./retroSchemas";

export type RetroHistoryListProps = {
  summaries: RetroSummary[];
  onSelect: (month: string) => void;
  selectedMonth: string | null;
};

type YearGroup = { year: string; rows: RetroSummary[] };

// Summaries arrive newest-month-first (retro_repo.go: "ORDER BY r.month
// DESC"), and ISO month strings ("2026-06") sort the same way lexically as
// chronologically, so every row belonging to one year is already contiguous
// in the array -- this is a single pass, never a re-sort (the same "server
// order preserved" rule RetrosPage.tsx's Task-10 stand-in already stated for
// this same data).
function groupByYear(summaries: RetroSummary[]): YearGroup[] {
  const groups: YearGroup[] = [];
  for (const summary of summaries) {
    const year = summary.month.slice(0, 4);
    const current = groups[groups.length - 1];
    if (current && current.year === year) {
      current.rows.push(summary);
    } else {
      groups.push({ year, rows: [summary] });
    }
  }
  return groups;
}

// One row, both shapes a retro can be in: a finished retro shows only the
// clauses it actually has (design's own rule -- never "0 actions", never
// empty quotation marks), a draft shows the in-progress label instead
// (retroCopy.ts's own draftInProgress comment). Rendered as a real <button>,
// not a div with an onClick, because selecting a row is this component's one
// piece of real interactivity -- Modal.test.tsx's own file lists the
// keyboard-invisible-focus shape this codebase has shipped before from
// exactly that anti-pattern.
function RetroHistoryRow({
  summary,
  selected,
  onSelect,
}: {
  summary: RetroSummary;
  selected: boolean;
  onSelect: (month: string) => void;
}) {
  // Two independent channels, deliberately: the BACKGROUND says what the
  // retro is (a draft is tinted "in progress"), the BORDER says which month
  // the detail panel is showing.
  //
  // They used to share the background, and a household reported the result:
  // with August in progress and July clicked, the highlight still read as
  // August. The draft was tinted unconditionally so it looked selected
  // forever, a selected finished row only added a second and weaker tint
  // beside it, and a selected draft was pixel-identical to an unselected one
  // -- clicking it changed nothing anyone could see. `aria-pressed` was
  // right the whole time, which is why no test and no browser walk caught
  // it: the semantics were correct and only the picture lied.
  //
  // border-accent for selection is NewSpaceModal.tsx's own pattern for a
  // selectable card whose background already means something else. The
  // transparent border on every other row keeps the width reserved, so
  // selecting one does not shift the list by a pixel.
  const background = summary.finished ? "" : "bg-callout";
  const border = selected ? "border-accent" : "border-transparent";

  return (
    <button
      type="button"
      // Per-month, not the fixed "retro-summary-row"/"retro-draft-row"
      // task-10-report.md asked this replacement to keep. That request held
      // for a stand-in that only ever rendered one row on screen at a time
      // in a test; a real list can hold nineteen rows across two years (see
      // this file's own disclosure test), and getByTestId requires a unique
      // match -- a shared testid across every finished row would break the
      // moment two of them exist together, which is the ordinary case, not
      // an edge one. No separate role-only testid alongside it: add one only
      // once a test actually needs to query the row-type distinction without
      // the month, the same "extend when the need arrives" rule
      // `CLAUDE.md`'s `BankSyncProvider` note gives for a port.
      data-testid={`retro-row-${summary.month}`}
      onClick={() => onSelect(summary.month)}
      aria-pressed={selected}
      // min-h-11/sm:min-h-0: TransactionFilters.tsx's own SELECT_CLASS
      // comment has the measured reason py-2 alone falls 5px short of the
      // 44px floor on a phone. These rows are interactive controls now that
      // they carry onSelect -- Task 10's stand-in correctly noted no floor
      // applied to a row nothing could be clicked into yet; that is no
      // longer true once this component replaces it.
      className={`min-h-11 w-full rounded-lg border px-3 py-2 text-left text-[13.5px] transition-colors duration-[var(--transition-state)] hover:bg-canvas sm:min-h-0 ${background} ${border}`}
    >
      <div className="font-semibold text-ink">{monthYearLabel(summary.month)}</div>
      {summary.finished ? (
        <div className="mt-0.5 text-[11.5px] text-muted">
          {[
            summary.mood !== null ? `Mood ${summary.mood}/5` : null,
            // Task 10's own stand-in rendered this clause unconditionally
            // (`${summary.actionCount} action(s)` with no guard), which
            // would have shown "0 actions" on a finished retro nobody added
            // an action to -- exactly the defect family this task's brief
            // calls out. Guarded the same way the quote clause already was.
            summary.actionCount > 0
              ? `${summary.actionCount} action${summary.actionCount === 1 ? "" : "s"}`
              : null,
            summary.quote ? `"${summary.quote}"` : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </div>
      ) : (
        <div className="mt-0.5 text-[11.5px] font-semibold text-accent">{RETRO_COPY.draftInProgress}</div>
      )}
    </button>
  );
}

export function RetroHistoryList({ summaries, onSelect, selectedMonth }: RetroHistoryListProps) {
  // Which collapsed years a reader has expanded this session. The newest
  // year (see below) needs no entry here -- it renders expanded from the
  // first paint, matching the design's "current year expanded" rule.
  const [expandedYears, setExpandedYears] = useState<ReadonlySet<string>>(new Set());
  const groups = groupByYear(summaries);

  return (
    <div className="flex flex-col gap-1">
      {groups.map((group, index) => {
        // "Current year" here is the newest year present in the data, not
        // `new Date().getFullYear()` -- the design's own wording ("the
        // current year expanded") is written for the ordinary case where a
        // household's freshest retro *is* in the real current year. Reading
        // it as "the data's own newest year" instead keeps that same
        // promise (the freshest history is always visible with no click)
        // even in the one stretch each year before this month's retro
        // exists yet, and it keeps this component a pure function of its
        // props -- nothing to fake a clock for in a test.
        const expanded = index === 0 || expandedYears.has(group.year);

        if (!expanded) {
          return (
            <button
              key={group.year}
              type="button"
              onClick={() =>
                setExpandedYears((prev) => {
                  const next = new Set(prev);
                  next.add(group.year);
                  return next;
                })
              }
              // Same 44px reasoning as the row buttons above -- this is the
              // brief's own named example of a control the floor applies to.
              className="min-h-11 rounded-lg px-3 py-2 text-left text-[12.5px] font-semibold text-accent transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off sm:min-h-0"
            >
              {RETRO_COPY.showOlderYear(group.year, group.rows.length)}
            </button>
          );
        }

        return group.rows.map((summary) => (
          <RetroHistoryRow
            key={summary.id}
            summary={summary}
            selected={summary.month === selectedMonth}
            onSelect={onSelect}
          />
        ));
      })}
    </div>
  );
}
