// The design's four-card row at the top of the set-state Budget screen.
// Pure presentation: every figure arrives already in minor units from
// BudgetPage, and this file's only job is formatMoney plus which cards to
// show. No fetching, no derived math beyond string formatting -- the sign on
// Remaining and the pace figure both come straight from the wire
// (BudgetService.Month already computed them; re-deriving here would be a
// second place that math could drift from the server's own answer).
import { BUDGET_COPY } from "./budgetCopy";
import { formatMoney } from "./formatMoney";

function StatCard({
  testId,
  label,
  value,
  valueClassName,
}: {
  testId: string;
  label: string;
  value: string;
  valueClassName?: string;
}) {
  return (
    <div data-testid={testId} className="rounded-xl border border-hairline bg-card px-[22px] py-[18px]">
      <div className="text-xs text-muted">{label}</div>
      <div className={`mt-1.5 text-2xl font-semibold tracking-[-0.02em] ${valueClassName ?? "text-ink"}`}>
        {value}
      </div>
    </div>
  );
}

export function BudgetStatCards({
  currency,
  symbol,
  budgetedMinor,
  spentMinor,
  remainingMinor,
  dailyPaceMinor,
  dailyPaceOk,
  spentSoFar,
}: {
  currency: string;
  symbol?: string;
  budgetedMinor: number;
  spentMinor: number;
  remainingMinor: number;
  dailyPaceMinor: number;
  // Gates the whole fourth card. Server-computed (usecase/budget.go's own
  // comment: hidden when Remaining <= 0 or the viewed month isn't current),
  // so this component only reads the flag rather than re-deriving it from
  // remainingMinor -- a page viewing a past month with a positive Remaining
  // must still hide this card, and only the server knows "is this the
  // current month."
  dailyPaceOk: boolean;
  // True while the viewed month still has days left to spend in -- controls
  // whether "Spent" reads "Spent so far" (BUDGET_COPY's own comment on why).
  spentSoFar: boolean;
}) {
  return (
    // 320px floor violation (measured: the Daily pace card's own value --
    // e.g. "S$214.95/day" -- laid out at 148px, wider than the 92px of
    // content width two unprefixed columns leave after this card's own
    // px-[22px] padding, pushing the document to a 339px scrollWidth). The
    // base was `grid-cols-2` -- too narrow for a currency figure at the
    // floor -- not the pre-existing `md:grid-cols-4` (that breakpoint stays;
    // FieldPair.tsx's own comment on why this file was left out of its
    // shared two-column pattern is about `md` specifically, not about this
    // floor). One column at the floor, `sm:grid-cols-2` restoring the
    // two-up layout once there is room for it, same shape FieldPair.tsx uses.
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-4">
      <StatCard
        testId="budget-stat-budgeted"
        label={BUDGET_COPY.budgeted}
        value={formatMoney(budgetedMinor, currency, symbol)}
      />
      <StatCard
        testId="budget-stat-spent"
        label={spentSoFar ? BUDGET_COPY.spentSoFar : BUDGET_COPY.spent}
        value={formatMoney(spentMinor, currency, symbol)}
      />
      <StatCard
        testId="budget-stat-remaining"
        label={BUDGET_COPY.remaining}
        value={formatMoney(remainingMinor, currency, symbol)}
        // Negative renders as over rather than clamping (spec's formulas
        // table, Remaining row) -- the colour is the only place that
        // distinction shows on this card, so it has to follow the sign, not
        // just default to the "healthy" green.
        valueClassName={remainingMinor < 0 ? "text-danger" : "text-accent"}
      />
      {dailyPaceOk && (
        <StatCard
          testId="budget-stat-pace"
          label={BUDGET_COPY.dailyPace}
          value={`${formatMoney(dailyPaceMinor, currency, symbol)}${BUDGET_COPY.perDayLeft}`}
        />
      )}
    </div>
  );
}
