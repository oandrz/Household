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
import { ApiError } from "../../api/client";
import { useCurrencies } from "../auth/useAuth";
import { Modal } from "../../components/Modal";
import { PageContainer } from "../../components/PageContainer";
import { BudgetHistoryModal } from "./BudgetHistoryModal";
import { BudgetModal } from "./BudgetModal";
import { BUDGET_COPY } from "./budgetCopy";
import { BudgetByPerson } from "./BudgetByPerson";
import { BudgetCategoryGrid } from "./BudgetCategoryGrid";
import { BudgetRolloverCard } from "./BudgetRolloverCard";
import { BudgetStatCards } from "./BudgetStatCards";
import { familyOfFourTemplate, fiftyThirtyTwentyTemplate, type TemplatePrefill } from "./budgetTemplates";
import { formatMoney } from "./formatMoney";
import { currentMonth } from "./month";
import { useBudget } from "./useBudget";
import { useBudgetHistory } from "./useBudgetHistory";
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
  // The History modal (Task 15). `useBudgetHistory`'s own `enabled` gate is
  // this flag directly -- opening History is the only reason this page ever
  // needs the /budgets/history request, so closing it stops the query
  // mattering rather than firing on every Budget screen visit.
  const [historyOpen, setHistoryOpen] = useState(false);
  const history = useBudgetHistory(historyOpen);

  if (budget.loading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }
  if (budget.error) {
    // GET /budgets/{month} is money AND owner-gated, identically to GET
    // /goals (router.go's own comment on the whole `txn` group) -- a
    // limited member holding money reaches this route (the sidebar link
    // and the /money route guard both check only the capability, never the
    // role) and the request answers 403. Branching on the real status, not
    // a second useMe() role check -- GoalsPage.tsx's own comment on its
    // identical branch: a role check here would be a second source of
    // truth that could disagree with what the server actually decided.
    const status = budget.error instanceof ApiError ? budget.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="budget-owner-only" className="m-9 rounded-xl border border-hairline bg-card p-[22px]">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{BUDGET_COPY.title}</h1>
          <h2 className="mt-4 text-xs text-muted">{BUDGET_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{BUDGET_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="budget-load-error" className="p-9 text-xs text-danger">
        {BUDGET_COPY.loadError}
      </p>
    );
  }
  // budget.error is null and budget.loading is false here, so data is
  // present -- TanStack Query's own contract. This guards the type only,
  // the same defensive shape GoalsPage.tsx's `!goals.data` guard uses.
  if (!budget.data) {
    return null;
  }

  const data = budget.data;
  const symbol = currencies.data?.currencies.find((c) => c.code === data.currency)?.symbol;
  // daysLeft is 0 for a past month (spec's formulas table); a future or the
  // current month both have at least today itself left to spend in. Reused
  // for both the "so far" wording below and the header's own days-left line.
  const spentSoFar = data.daysLeft > 0;
  const overSentence = overCategorySentence(data.overCount, data.categories);
  const categoryList = categories.data ?? [];
  // The server's own answer to "is this month closed" (spec's formulas
  // table: daysLeft is 0 for a past month, never for the current or a
  // future one) -- passed straight through to BudgetRolloverCard.tsx as
  // `closed` rather than letting that component recompute the same fact a
  // second way (that file's own header comment explains why: an earlier
  // version did, and it silently disagreed with this exact figure).
  const monthClosed = data.daysLeft === 0;
  // Whether BudgetRolloverCard.tsx could show anything at all -- purely to
  // decide the surrounding `budget-insight` box's own visibility (a closed
  // month with dailyPaceOk false and no over-category otherwise has nothing
  // to put in that box). Checked against `rolloverAmountMinor`, not
  // `rolloverGoalId` -- the two move together, but BudgetRolloverCard.tsx's
  // own "done" branch is gated on the amount now (Finding 1's fix), so this
  // visibility check uses the same field rather than a second one that
  // happens to agree with it.
  const rolloverMayShow = monthClosed && (data.remainingMinor > 0 || data.rolloverAmountMinor !== null);

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
  // The Edit-budget entry point for a month that already has a budget --
  // BudgetModal.tsx's own header comment anticipated this exact function
  // ("a future 'Edit budget' entry point (Task 15) for an *existing* budget
  // would normalise the same way"). `data.budget` is never null on this
  // path (the header button that calls this only renders once the screen
  // has already branched into the populated state below), but the guard
  // stays rather than a non-null assertion -- the same "fail closed on a
  // value you did not just construct" instinct as everywhere else here.
  function openEditBudget() {
    if (!data.budget) return;
    setModal({
      prefill: { expectedIncomeMinor: data.budget.expectedIncomeMinor, lines: data.budget.lines, missing: [] },
      awaitingIncome: false,
    });
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
    <PageContainer data-testid="budget-page">
      {/* flex-wrap: the same header-row overflow BillsPage.tsx's own header
          comment describes -- the month picker plus History and Edit budget
          never had room beside the title at 320px. Not caught before this
          task because no browser walk had a real budget (with History and
          Edit budget both rendering) at a phone width until now. */}
      <div className="flex flex-wrap items-start justify-between gap-4">
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
        {/* The design's header row groups the picker chip with History and
            Edit budget in one flex container -- the picker itself stays
            visible in every state (a household should be able to navigate
            to a month with a budget from one that has none, e.g. to reach
            "Import last month"'s source month), but History and Edit budget
            only render once there is a budget to show history against or
            edit -- matching the design's own mockup, which has no header
            controls at all on the empty-state screen. */}
        <div className="flex items-center gap-2">
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
          {data.budget !== null && (
            <>
              <button
                type="button"
                data-testid="budget-history-button"
                onClick={() => setHistoryOpen(true)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-muted"
              >
                {BUDGET_COPY.history}
              </button>
              <button
                type="button"
                data-testid="budget-edit-button"
                onClick={openEditBudget}
                className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
              >
                {BUDGET_COPY.editBudget}
              </button>
            </>
          )}
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

          {/* xl, not the shell's lg: at 1024 this column would be 441px, and
              BudgetCategoryGrid's own sm:grid-cols-2 (a viewport breakpoint,
              not a container query) still goes two-up inside it regardless --
              measured 180px-ish sub-columns where "Kids & school" and its
              amount both wrapped their own text. BillsPage's identical
              [1.7fr_1fr] row keeps lg: its left column is a single-column
              list with nothing to compound with. */}
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-[1.7fr_1fr]">
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

              {(data.dailyPaceOk || overSentence || rolloverMayShow) && (
                <div data-testid="budget-insight" className="rounded-xl bg-callout px-5 py-[18px]">
                  {data.dailyPaceOk && (
                    <p className="text-[13px] font-semibold text-accent">
                      {BUDGET_COPY.onPaceToSave(formatMoney(data.remainingMinor, data.currency, symbol))}
                    </p>
                  )}
                  {overSentence && (
                    <p className="mt-1.5 text-[12px] leading-relaxed text-accent-dark">{overSentence}</p>
                  )}
                  {rolloverMayShow && (
                    <div className="mt-1.5">
                      <BudgetRolloverCard
                        month={data.month}
                        closed={monthClosed}
                        remainingMinor={data.remainingMinor}
                        rolloverAmountMinor={data.rolloverAmountMinor}
                        currency={data.currency}
                        rolledOverTo={data.rolloverGoalId}
                        symbol={symbol}
                        excludedNoRate={data.excludedNoRate}
                        // No-op: useBudget.ts's own rollOver mutation
                        // already invalidates this month and /goals on
                        // success (its own comment) -- `data` above already
                        // flows from the same query that refetch updates,
                        // so this page has nothing further to do once it
                        // fires.
                        onRolledOver={() => {}}
                      />
                    </div>
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

      {/* Shared across both branches above -- a template/blank click from
          the empty state and an Edit-budget click from the populated state
          both open the same modal, so it renders once here rather than
          twice (one copy per branch would drift). BudgetModal.tsx's own
          header comment explains why `initial` is always a
          `TemplatePrefill`, never `null`: "Create your first budget" and
          Edit-budget's own `openEditBudget` above both normalise into the
          same shape rather than the modal branching on a union internally --
          there is no behavioural difference between "no prefill" and "a
          prefill with zero lines and nothing missing." */}
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

      {/* The History modal (Task 15). Rendered only while `historyOpen` is
          true -- `useBudgetHistory`'s own `enabled: historyOpen` above means
          the request that backs it hasn't even fired before this point, so
          a loading branch is real, not theoretical, on every open (never
          served from a warm cache the way `useBudget`'s prevMonth query
          sometimes is). Picking a row both switches this page's own month
          state and closes the modal -- "the design's full breakdown is the
          page itself" (this file's own header comment), not a second view
          inside the modal. */}
      {historyOpen &&
        (history.error ? (
          <Modal open onClose={() => setHistoryOpen(false)} title={BUDGET_COPY.historyModalTitle}>
            <p role="alert" className="text-xs text-danger" data-testid="budget-history-error">
              {BUDGET_COPY.loadError}
            </p>
          </Modal>
        ) : history.data ? (
          <BudgetHistoryModal
            months={history.data.months}
            currency={data.currency}
            symbol={symbol}
            onPickMonth={(pickedMonth) => {
              setMonth(pickedMonth);
              setHistoryOpen(false);
            }}
            onClose={() => setHistoryOpen(false)}
          />
        ) : (
          <Modal open onClose={() => setHistoryOpen(false)} title={BUDGET_COPY.historyModalTitle}>
            <p className="text-xs text-muted" data-testid="budget-history-loading">
              Loading…
            </p>
          </Modal>
        ))}
    </PageContainer>
  );
}
