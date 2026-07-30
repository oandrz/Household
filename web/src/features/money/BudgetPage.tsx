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
// The empty state (`budget: null`) is the design's real "Create your first
// budget" panel plus its two live templates ("Family of four", "50 / 30 /
// 20") and the conditional third ("Import last month"). Template math lives
// in budgetTemplates.ts, kept pure and tested on its own -- this file's job
// is only to wire a click to a prefill and hand it off to the modal.
//
// The modal itself is Task 14's. Until then, a template click (or "Create
// your first budget") opens `modal` state below and renders a minimal
// stub branch carrying the prefill's line count in a testid -- the seam
// Task 14 replaces, not something this task tries to half-build.
import { useState } from "react";
import { useCurrencies } from "../auth/useAuth";
import { BudgetModal } from "./BudgetModal";
import { BUDGET_COPY } from "./budgetCopy";
import { BudgetByPerson } from "./BudgetByPerson";
import { BudgetCategoryGrid } from "./BudgetCategoryGrid";
import { BudgetStatCards } from "./BudgetStatCards";
import { familyOfFourTemplate, fiftyThirtyTwentyTemplate, type TemplatePrefill } from "./budgetTemplates";
import { formatMoney } from "./formatMoney";
import { useBudget } from "./useBudget";
import { useCategories } from "./useTransactions";
import type { BudgetMonthResponse } from "./budgetSchemas";

// The modal handoff Task 14 consumes: `prefill: null` is a blank budget
// ("Create your first budget"), a `TemplatePrefill` is a template's
// computed starting point. `awaitingIncome` is set only by the 50/30/20
// card -- its prefill has zero lines until an income figure exists, and
// the modal (Task 14) uses this flag, not "lines.length === 0" alone, to
// decide whether to show the income prompt (a household with genuinely no
// matching categories would also have zero lines, for an unrelated reason).
type ModalState = {
  prefill: TemplatePrefill | null;
  awaitingIncome: boolean;
};

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

// "2026-06" -> "June". The design's own wording (Household Dashboard.dc.html
// line 438: "No budget set for July yet"; the Import card's own "Copy
// June's caps") names only the month, both times, not the year -- "this
// month"/"last month" read as unambiguous without one. monthLabel above
// stays the year-qualified label everywhere else on this screen (the
// header's month chip and its "N days left in July 2026" line), where a
// household scrolling through history needs the year to tell which July
// it's on.
function monthNameOnly(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "long" });
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
  // Unconditional, not gated behind `data.budget === null`: React's rules of
  // hooks forbid calling it only inside the empty-state branch below (that
  // branch doesn't exist yet at this point in the render), and the template
  // cards need this list ready the instant a household clicks one, not a
  // second request kicked off only after the click. This is deliberately
  // `useCategories()`'s plain, non-archived list -- not `data.categories`
  // (useBudget's per-month view, which includes archived categories so an
  // old cap still renders on its own row). A template should never offer to
  // prefill a cap onto a category the household archived; `data.categories`
  // would let one leak through, and it uses `categoryId`, not the `id` the
  // templates key their name-matching on.
  const categories = useCategories();
  // Task 14 owns the real modal; this page only decides what it should
  // open pre-filled with. `null` means no modal is open.
  const [modal, setModal] = useState<ModalState | null>(null);

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
  const categoryList = categories.data ?? [];

  function openBlank() {
    setModal({ prefill: null, awaitingIncome: false });
  }
  function openFamilyOfFour() {
    setModal({ prefill: familyOfFourTemplate(categoryList), awaitingIncome: false });
  }
  function openFiftyThirtyTwenty() {
    // Called with 0, not the previous month's or any guessed income --
    // fiftyThirtyTwentyTemplate treats that as "blank" and returns zero
    // lines (budgetTemplates.ts's own comment), which is exactly the
    // waiting-for-income state the modal (Task 14) opens into.
    setModal({ prefill: fiftyThirtyTwentyTemplate(categoryList, 0), awaitingIncome: true });
  }
  function openImportLastMonth() {
    if (!budget.prevMonthBudget) return;
    // The previous month's lines already reference real categoryIds --
    // no name-mapping needed the way the two templates above need it, so
    // `missing` is trivially empty here.
    setModal({
      prefill: {
        expectedIncomeMinor: budget.prevMonthBudget.expectedIncomeMinor,
        lines: budget.prevMonthBudget.lines,
        missing: [],
      },
      awaitingIncome: false,
    });
  }

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
          data-testid="budget-empty-state"
          className="rounded-xl border border-hairline bg-card p-16 text-center"
        >
          <div className="text-[19px] font-semibold tracking-[-0.01em] text-ink">
            {BUDGET_COPY.emptyHeadline(monthNameOnly(data.month))}
          </div>
          <p className="mx-auto mt-2 max-w-[420px] text-[13.5px] leading-relaxed text-muted">
            {BUDGET_COPY.emptyBody}
          </p>
          <div className="mt-6 flex justify-center gap-2.5">
            <button
              type="button"
              data-testid="budget-create-blank"
              onClick={openBlank}
              className="rounded-lg bg-accent px-5 py-2.5 text-[13px] font-semibold text-white"
            >
              {BUDGET_COPY.createFirstBudget}
            </button>
            {/* The design's own markup wires both buttons to the same
                `openBudget` handler -- the template cards are already
                rendered below, not hidden behind this button, so "Start
                from a template" is a second, lower-emphasis way into the
                same blank modal rather than a distinct flow of its own. */}
            <button
              type="button"
              data-testid="budget-start-from-template"
              onClick={openBlank}
              className="rounded-lg border border-callout-border bg-callout px-5 py-2.5 text-[13px] font-semibold text-accent"
            >
              {BUDGET_COPY.startFromTemplate}
            </button>
          </div>

          <div className="mx-auto mt-6 grid max-w-[560px] grid-cols-1 gap-2.5 text-left sm:grid-cols-3">
            <button
              type="button"
              data-testid="budget-template-family-of-four"
              onClick={openFamilyOfFour}
              className="rounded-[10px] border border-hairline p-3.5 text-left hover:border-accent hover:bg-callout"
            >
              <div className="text-[13px] font-semibold text-ink">{BUDGET_COPY.templateFamilyOfFour}</div>
              <div className="mt-0.5 text-[11.5px] text-muted">
                {BUDGET_COPY.templateFamilyOfFourSubtitle(data.currency)}
              </div>
            </button>
            <button
              type="button"
              data-testid="budget-template-fifty-thirty-twenty"
              onClick={openFiftyThirtyTwenty}
              className="rounded-[10px] border border-hairline p-3.5 text-left hover:border-accent hover:bg-callout"
            >
              <div className="text-[13px] font-semibold text-ink">{BUDGET_COPY.templateFiftyThirtyTwenty}</div>
              <div className="mt-0.5 text-[11.5px] text-muted">
                {BUDGET_COPY.templateFiftyThirtyTwentySubtitle}
              </div>
            </button>
            {/* "Import last month" is the one card that isn't always
                offered: spec decision 4, a new month starts unset, and this
                card only makes sense once there's a previous month's budget
                to copy. `prevMonthHasBudget` is read as exactly `true` here
                (never on the `undefined` still-loading state), so the card
                cannot flash in and then disappear once the request settles. */}
            {budget.prevMonthHasBudget === true && (
              <button
                type="button"
                data-testid="budget-template-import-last-month"
                onClick={openImportLastMonth}
                className="rounded-[10px] border border-hairline p-3.5 text-left hover:border-accent hover:bg-callout"
              >
                <div className="text-[13px] font-semibold text-ink">{BUDGET_COPY.templateImportLastMonth}</div>
                <div className="mt-0.5 text-[11.5px] text-muted">
                  {BUDGET_COPY.templateImportLastMonthSubtitle(monthNameOnly(budget.prevMonth))}
                </div>
              </button>
            )}
          </div>

          {/* BudgetModal.tsx's own header comment explains why `initial` is
              always a `TemplatePrefill`, never `null`: "Create your first
              budget" (modal.prefill === null) is normalised here into the
              blank shape rather than the modal branching on a union
              internally -- there is no behavioural difference between "no
              prefill" and "a prefill with zero lines and nothing missing." */}
          {modal && (
            <BudgetModal
              month={data.month}
              initial={modal.prefill ?? { expectedIncomeMinor: null, lines: [], missing: [] }}
              categories={categoryList}
              awaitingIncome={modal.awaitingIncome}
              onClose={() => setModal(null)}
              onSaved={() => setModal(null)}
            />
          )}
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
