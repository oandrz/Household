// The Budget screen (spec's "Set state," decision-table row 2): four stat
// cards, the Categories grid, Spending by person and an insight card.
// Follows FinancesPage.tsx/TransactionsPage.tsx's own shape -- this page
// only composes; fetch orchestration lives in useBudget.ts (spec decision
// 11), and every card component below takes already-parsed props and does
// no fetching of its own.
//
// Month state lives here, not in the hook: useBudget(month) is called with
// whatever this page currently has selected, so a month change is just a
// state update that flows into the hook's own query key -- useBudget.ts's
// own comment on why ["budget", month] makes a month switch impossible to
// show one month's figures under another month's label.
//
// The empty state (`budget: null`) gets only a placeholder branch here.
// Task 13 builds the design's real "Create your first budget" panel and
// templates; building it twice, once as a throwaway placeholder and once for
// real, is the kind of work this project's own docs warn against.
import { useState } from "react";
import { useCurrencies } from "../auth/useAuth";
import { BUDGET_COPY } from "./budgetCopy";
import { BudgetByPerson } from "./BudgetByPerson";
import { BudgetCategoryGrid } from "./BudgetCategoryGrid";
import { BudgetStatCards } from "./BudgetStatCards";
import { formatMoney } from "./formatMoney";
import { useBudget } from "./useBudget";
import type { BudgetMonthResponse } from "./budgetSchemas";

// Local calendar date, never toISOString() -- the same reasoning as
// AccountModal.tsx's own today(): a UTC conversion can read back yesterday's
// (or tomorrow's) month for a household west/east of UTC.
function currentMonth(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}

// Shifts a "YYYY-MM" string by whole months, for the ‹ › picker below. Built
// through Date's own (year, monthIndex, 1) constructor -- the day is fixed
// at 1, so there is no "day 31 doesn't exist next month" edge to handle the
// way a calendar-day shift would have.
function shiftMonth(month: string, delta: number): string {
  const [year, monthNum] = month.split("-").map(Number);
  const shifted = new Date(year, monthNum - 1 + delta, 1);
  return `${shifted.getFullYear()}-${String(shifted.getMonth() + 1).padStart(2, "0")}`;
}

// "2026-07" -> "July 2026". Parsed onto day 2, matching TransactionsPage's
// own monthLabel comment on why day 1 is the wrong anchor at a negative UTC
// offset.
function monthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "long",
    year: "numeric",
  });
}

// The insight card's over-category sentence, derived from `overCount` --
// the server's own computed figure (BudgetService.Month) -- rather than a
// second count taken by filtering `categories` for `over` here. The two
// would usually agree, but overCount is the one number this screen treats
// as ground truth everywhere else (the four stat cards, the pace figures),
// so this stays consistent with that rather than opening a second seam that
// could quietly disagree with it.
function overCategorySentence(
  overCount: number,
  categories: BudgetMonthResponse["categories"],
): string | null {
  if (overCount === 0) return null;
  if (overCount === 1) {
    const overCategory = categories.find((c) => c.over);
    return overCategory ? BUDGET_COPY.onlyCategoryOver(overCategory.name) : null;
  }
  return BUDGET_COPY.categoriesOver(overCount);
}

export function BudgetPage() {
  const [month, setMonth] = useState(() => currentMonth());
  const budget = useBudget(month);
  const currencies = useCurrencies();

  if (budget.loading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }
  if (budget.error || !budget.data) {
    return (
      <p role="alert" className="p-9 text-xs text-danger">
        {BUDGET_COPY.loadError}
      </p>
    );
  }

  const data = budget.data;
  const symbol = currencies.data?.currencies.find((c) => c.code === data.currency)?.symbol;
  // daysLeft is 0 for a past month (spec's formulas table); a future or the
  // current month both have at least today itself left to spend in. Reused
  // for both the "so far" wording below and the header's own days-left line.
  const spentSoFar = data.daysLeft > 0;
  const overSentence = overCategorySentence(data.overCount, data.categories);

  return (
    <div className="flex flex-col gap-5 px-9 py-8" data-testid="budget-page">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{BUDGET_COPY.title}</h1>
          {/* Always names the month (spec screen state 4: "the header still
              names the month," even for a spend-without-budget or past-month
              view) -- monthText below is never empty, so a past month with a
              real budget (percentOk true, daysLeft 0) still reads "66% used
              · July 2026" instead of losing the month name entirely. */}
          <p data-testid="budget-subtitle" className="mt-1 text-[13px] text-muted">
            {/* Budgeted = 0 hides the percent figure rather than showing
                NaN/Infinity (spec's formulas table, "66% used" row) -- this
                just reads the server's own percentOk flag instead of
                re-deriving the same guard from budgetedMinor here. */}
            {data.percentOk && (
              <span data-testid="budget-percent-used">{BUDGET_COPY.percentUsed(data.percentUsed)}</span>
            )}
            {data.percentOk && " · "}
            {spentSoFar
              ? BUDGET_COPY.daysLeftInMonth(data.daysLeft, monthLabel(data.month))
              : monthLabel(data.month)}
          </p>
        </div>
        <div className="flex items-center gap-3.5 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] text-muted">
          <button
            type="button"
            aria-label="Previous month"
            onClick={() => setMonth((current) => shiftMonth(current, -1))}
            className="text-muted"
          >
            ‹
          </button>
          <span className="font-semibold text-ink">{monthLabel(data.month)}</span>
          <button
            type="button"
            aria-label="Next month"
            onClick={() => setMonth((current) => shiftMonth(current, 1))}
            className="text-muted"
          >
            ›
          </button>
        </div>
      </div>

      {data.budget === null ? (
        <div
          data-testid="budget-empty-placeholder"
          className="rounded-xl border border-hairline bg-card p-16 text-center text-sm text-muted"
        >
          {BUDGET_COPY.emptyPlaceholder}
        </div>
      ) : (
        <>
          <BudgetStatCards
            currency={data.currency}
            symbol={symbol}
            budgetedMinor={data.budgetedMinor}
            spentMinor={data.spentMinor}
            remainingMinor={data.remainingMinor}
            dailyPaceMinor={data.dailyPaceMinor}
            dailyPaceOk={data.dailyPaceOk}
            spentSoFar={spentSoFar}
          />

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1.7fr_1fr]">
            <div className="rounded-xl border border-hairline bg-card p-[22px]">
              <div className="mb-[18px] flex items-baseline justify-between">
                <h2 className="text-sm font-semibold text-ink">{BUDGET_COPY.categories}</h2>
              </div>
              <BudgetCategoryGrid categories={data.categories} currency={data.currency} symbol={symbol} />
            </div>

            <div className="flex flex-col gap-4">
              <div className="rounded-xl border border-hairline bg-card p-[22px]">
                <h2 className="mb-4 text-sm font-semibold text-ink">{BUDGET_COPY.spendingByPerson}</h2>
                <BudgetByPerson people={data.byPerson} currency={data.currency} symbol={symbol} />
              </div>

              {(data.dailyPaceOk || overSentence) && (
                <div data-testid="budget-insight" className="rounded-xl bg-callout px-5 py-[18px]">
                  {data.dailyPaceOk && (
                    <p className="text-[13px] font-semibold text-accent">
                      {BUDGET_COPY.onPaceToSave(formatMoney(data.remainingMinor, data.currency, symbol))}
                    </p>
                  )}
                  {overSentence && (
                    <p className="mt-1.5 text-[12px] leading-relaxed text-accent-dark">{overSentence}</p>
                  )}
                </div>
              )}
            </div>
          </div>

          {data.excludedNoRate > 0 && (
            <p data-testid="budget-excluded-no-rate" className="text-xs text-muted">
              {BUDGET_COPY.excludedNoRate(data.excludedNoRate)}
            </p>
          )}
        </>
      )}
    </div>
  );
}
