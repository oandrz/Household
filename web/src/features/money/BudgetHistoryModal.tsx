// The History modal (design spec's "Budget history" section): three summary
// cards over closed months, then one row per month with a spent/budgeted
// bar. Pure presentation, the same shape BudgetCategoryGrid.tsx/BudgetByPerson.tsx
// take -- `months` arrives already fetched (BudgetPage.tsx owns the
// `useBudgetHistory` query, gated on this modal being open), and this file's
// only job is the summary math and formatting.
//
// The shared `components/Modal` primitive is fixed at `w-[420px]` (fifteen
// other call sites depend on that; see Modal.tsx's own header comment on why
// it exists as a shared primitive at all) -- narrower than the design's own
// 660px, four-column history table. Rather than widen the primitive for one
// caller, each row here stacks label/result above the bar above the spent
// figure (the same vertical shape BudgetCategoryGrid.tsx's own CategoryRow
// already uses at an even narrower column width), instead of the design's
// fixed `90px 1fr 96px 70px` grid, which has no room to breathe at 420px.
import { BUDGET_COPY } from "./budgetCopy";
import { formatMoney } from "./formatMoney";
import { Modal } from "../../components/Modal";
import { HISTORY_MONTHS } from "./useBudgetHistory";
import type { BudgetHistoryResponse } from "./budgetSchemas";

type HistoryMonth = BudgetHistoryResponse["months"][number];

// "2026-07" -> "Jul 2026", the row label's own short form -- distinct from
// BudgetPage.tsx's `monthLabel` ("July 2026", the header chip's long form).
// Kept local rather than imported: BudgetPage.tsx is the page this modal is
// opened from, not a module this modal should depend on (the same reasoning
// useBudgetHistory.ts's own header comment gives for staying out of
// useBudget.ts). Parsed onto day 2, the same UTC-offset safeguard every
// other month-formatting helper in this feature uses.
function shortMonthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "short", year: "numeric" });
}

// "2026-07" -> "July", for the in-progress footnote -- month only, the same
// convention BudgetPage.tsx's own `monthNameOnly` documents (a household
// scrolling a single footnote sentence doesn't need the year to tell which
// July it's reading about).
function monthNameOnly(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "long" });
}

// The bar's fill width -- the same shape BudgetCategoryGrid.tsx's own
// `barWidthPercent` guards (0/0 must never reach the DOM as `NaN%`), kept as
// its own copy rather than imported: that function's guard is phrased around
// `capMinor`, this one around `budgetedMinor`, and the two would read as
// coincidentally identical rather than actually coupled if one imported the
// other. `Math.min(100, ...)` caps the visual fill at 100% -- the design's
// own mockup draws May's bar at `width:104%`, which this deliberately does
// not copy: a bar wider than its own track is a mockup-only affordance, not
// something a browser should ever render.
function barWidthPercent(spentMinor: number, budgetedMinor: number): number {
  if (budgetedMinor <= 0) return spentMinor > 0 ? 100 : 0;
  return Math.min(100, (spentMinor / budgetedMinor) * 100);
}

// The current month's row always reads as in-progress, never as a verdict --
// there's nothing to judge "over" or "under" about a month that isn't over
// yet, so its bar stays the same accent colour regardless of how much of the
// cap it has already used. A closed month's bar turns the "over" colour only
// once it actually finished over cap.
function barColorClass(month: HistoryMonth): string {
  if (!month.closed) return "bg-accent";
  return month.spentMinor > month.budgetedMinor ? "bg-danger" : "bg-accent";
}

// Result = Spent minus Budgeted, signed -- budgetSchemas.ts's own comment on
// why this is computed client-side rather than sent on the wire. Positive
// (over cap) needs an explicit "+" -- formatMoney only ever emits a sign for
// negative amounts (its own U+2212 minus), never a "+" for positive ones, so
// this is the one place that has to add it.
function formatResult(resultMinor: number, currency: string, symbol?: string): string {
  if (resultMinor > 0) return `+${formatMoney(resultMinor, currency, symbol)}`;
  return formatMoney(resultMinor, currency, symbol);
}

function SummaryCard({ label, value, valueClassName }: { label: string; value: string; valueClassName?: string }) {
  return (
    <div className="rounded-[10px] border border-hairline bg-canvas px-3.5 py-2.5">
      <div className="text-[11px] text-muted">{label}</div>
      <div className={`mt-1 text-[15px] font-semibold tracking-[-0.01em] ${valueClassName ?? "text-ink"}`}>
        {value}
      </div>
    </div>
  );
}

function HistoryRow({
  month,
  currency,
  symbol,
  onPick,
}: {
  month: HistoryMonth;
  currency: string;
  symbol?: string;
  onPick: (month: string) => void;
}) {
  const width = barWidthPercent(month.spentMinor, month.budgetedMinor);
  const over = month.spentMinor > month.budgetedMinor;
  const resultMinor = month.spentMinor - month.budgetedMinor;

  return (
    <button
      type="button"
      data-testid="budget-history-row"
      onClick={() => onPick(month.month)}
      className="flex w-full flex-col gap-1.5 border-t border-hairline py-3 text-left first:border-t-0 hover:bg-canvas"
    >
      <div className="flex items-baseline justify-between gap-2 text-[13px]">
        <span className="font-semibold text-ink">{shortMonthLabel(month.month)}</span>
        {month.closed ? (
          <span
            data-testid="budget-history-row-result"
            className={`font-semibold ${over ? "text-danger" : "text-accent"}`}
          >
            {formatResult(resultMinor, currency, symbol)}
          </span>
        ) : (
          <span data-testid="budget-history-row-so-far" className="font-semibold text-muted">
            {BUDGET_COPY.historySoFar}
          </span>
        )}
      </div>
      <div className="h-[6px] overflow-hidden rounded-full bg-canvas">
        <div className={`h-full ${barColorClass(month)}`} style={{ width: `${width}%` }} />
      </div>
      <div className={`text-right text-[11.5px] ${!month.closed ? "text-muted" : over ? "text-danger" : "text-ink"}`}>
        {formatMoney(month.spentMinor, currency, symbol)}
        {!month.closed && "*"}
      </div>
    </button>
  );
}

export function BudgetHistoryModal({
  months,
  currency,
  symbol,
  onPickMonth,
  onClose,
}: {
  months: HistoryMonth[];
  currency: string;
  symbol?: string;
  onPickMonth: (month: string) => void;
  onClose: () => void;
}) {
  // Denominator for both avg cards and "months under budget": closed months
  // with a real cap total, not every closed month the server returned. A
  // closed month can only exist here at all if it had a budget row (the
  // server's own doc comment on History: "a month without a budget row is
  // simply absent, never zero-filled"), but a household can still have saved
  // a budget with every line removed -- `budgetedMinor === 0` -- and a month
  // like that has nothing to be "under" or "over" relative to, so it is
  // excluded from all three figures rather than dragging every average
  // toward zero. The current month is excluded from all three for a
  // different reason: it isn't over yet, so there's nothing to average or
  // judge about it -- its own row shows "so far" instead.
  const closedBudgeted = months.filter((m) => m.closed && m.budgetedMinor > 0);

  const avgSpendMinor =
    closedBudgeted.length === 0
      ? null
      : Math.round(closedBudgeted.reduce((sum, m) => sum + m.spentMinor, 0) / closedBudgeted.length);
  const avgSavedMinor =
    closedBudgeted.length === 0
      ? null
      : Math.round(
          closedBudgeted.reduce((sum, m) => sum + (m.budgetedMinor - m.spentMinor), 0) / closedBudgeted.length,
        );
  // "Under budget" reads spent <= budgeted as under -- a month that landed
  // exactly on its cap counts as a win, not a miss, the same direction
  // BudgetStatCards.tsx's own Remaining card treats `remainingMinor === 0`
  // (never coloured as over).
  const underCount = closedBudgeted.filter((m) => m.spentMinor <= m.budgetedMinor).length;

  const current = months.find((m) => !m.closed);

  return (
    <Modal open onClose={onClose} title={BUDGET_COPY.historyModalTitle}>
      <p className="text-xs text-muted">{BUDGET_COPY.historyModalSubtitle(HISTORY_MONTHS, currency)}</p>

      {months.length === 0 ? (
        <p className="mt-4 text-[13px] leading-relaxed text-muted" data-testid="budget-history-empty">
          {BUDGET_COPY.historyEmpty}
        </p>
      ) : (
        <>
          <div className="mt-4 grid grid-cols-3 gap-2">
            <SummaryCard
              label={BUDGET_COPY.historyAvgSpend}
              value={avgSpendMinor === null ? BUDGET_COPY.noValue : formatMoney(avgSpendMinor, currency, symbol)}
            />
            <SummaryCard
              label={BUDGET_COPY.historyAvgSaved}
              value={avgSavedMinor === null ? BUDGET_COPY.noValue : formatMoney(avgSavedMinor, currency, symbol)}
              valueClassName="text-accent"
            />
            <SummaryCard
              label={BUDGET_COPY.historyMonthsUnderBudget}
              value={BUDGET_COPY.historyMonthsUnderBudgetValue(underCount, closedBudgeted.length)}
            />
          </div>

          <div className="mt-3 flex flex-col">
            {months.map((month) => (
              <HistoryRow key={month.month} month={month} currency={currency} symbol={symbol} onPick={onPickMonth} />
            ))}
          </div>

          {current && (
            <p className="mt-2.5 text-[11.5px] text-muted">{BUDGET_COPY.historyFootnote(monthNameOnly(current.month))}</p>
          )}
        </>
      )}
    </Modal>
  );
}
