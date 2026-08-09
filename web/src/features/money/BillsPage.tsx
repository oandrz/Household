// The Bills screen: three stat cards, Due soon/Later/Paid this month, the
// All-caught-up panel, and "Show archived" -- the task brief's own five
// states plus archive/restore. Composition only, GoalsPage.tsx/BudgetPage.tsx's
// own shape: fetch orchestration lives in useBills.ts, and no `apiFetch` call
// belongs in this file.
//
// Due soon and Later are derived here from the server's own `dueSoon` flag
// on each bill (BillView's own comment: computed server-side "so the rule
// lives in exactly one place") -- this file only filters and preserves the
// array's own order, never recomputes the 30-day window. That order already
// matches the spec (`ORDER BY next_due NULLS LAST, name`), so an overdue
// bill's earlier date is what sorts it first; nothing here re-sorts.
//
// BillModal (Task 13), MarkPaidModal (Task 14) and SubscriptionsCard
// (Task 15) do not exist yet -- this task's own scope boundary. "+ Add bill"
// below renders with no handler rather than opening nothing when clicked (a
// button that does nothing on click is worse than no button, the same rule
// this task's brief states for every deferred control); BillRow's own
// `onEdit` is scaffolded but left unpassed for the identical reason. Both
// are Task 13's to wire, since its own file list only touches this file and
// BillModal.tsx -- BillRow.tsx has to carry the hook already, or Task 13
// would have nowhere to attach it without touching a file outside its scope.
import { useState } from "react";
import { useCurrencies } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { BillRow } from "./BillRow";
import { BillStatCards } from "./BillStatCards";
import { BILL_COPY, dayMonthLabel } from "./billCopy";
import { useArchiveBill, useBills, useRestoreBill } from "./useBills";

// "What month is it right now" -- read straight from the live clock, unlike
// BudgetPage.tsx's monthLabel/GoalCard.tsx's targetMonthLabel, which both
// anchor on day 2 to avoid a UTC-offset shift when *parsing a stored date
// string*. There is no stored string here to parse -- `new Date()` already
// names today in the caller's own local timezone -- so that anchor trick has
// nothing to guard against.
function currentMonthName(): string {
  return new Date().toLocaleDateString("en-US", { month: "long" });
}

export function BillsPage() {
  const [includeArchived, setIncludeArchived] = useState(false);
  const bills = useBills(includeArchived);
  const currencies = useCurrencies();
  const archiveBill = useArchiveBill();
  const restoreBill = useRestoreBill();
  // Scoped per bill id, not one page-wide flag -- useArchiveBill/useRestoreBill
  // are each one shared mutation instance, so a single mutation's own
  // isPending only ever reflects the most recently dispatched call
  // (AccountsPanel.tsx's own pendingIds carries the identical reasoning).
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());

  function trackPending(id: string, call: Promise<unknown>) {
    setPendingIds((prev) => new Set(prev).add(id));
    void call.finally(() => {
      setPendingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    });
  }

  // The only way a household reaches the archived view at all, and gets
  // back out of it -- this task's own reason for existing (see the header
  // comment above and docs/LEARNING.md pattern 15).
  function handleArchive(id: string) {
    trackPending(id, archiveBill.mutateAsync(id));
  }
  function handleRestore(id: string) {
    trackPending(id, restoreBill.mutateAsync(id));
  }

  if (bills.isLoading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }
  if (bills.error) {
    return (
      <p role="alert" data-testid="bills-load-error" className="p-9 text-xs text-danger">
        {BILL_COPY.loadError}
      </p>
    );
  }
  // bills.error is null and isLoading is false here, so data is present --
  // TanStack Query's own contract. This guards the type only, the same
  // defensive shape BudgetPage.tsx's `!budget.data` guard uses.
  if (!bills.data) {
    return null;
  }

  const data = bills.data;
  const { summary, paidThisMonth } = data;
  // `data.bills` is a union when includeArchived is true (BillsResponse's
  // own comment: the query string changes which rows the server includes,
  // not the wire shape) -- filtered here into the three groupings the page
  // renders, each filter preserving the server's own order.
  const liveBills = data.bills.filter((bill) => bill.archivedAt === null);
  const archivedBills = data.bills.filter((bill) => bill.archivedAt !== null);
  const dueSoonBills = liveBills.filter((bill) => bill.dueSoon);
  const laterBills = liveBills.filter((bill) => !bill.dueSoon);

  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;

  // FinancesPage.tsx's own FirstRunPanel reasoning, restated: gated on
  // *live* bills, not on `data.bills.length === 0 && !includeArchived`. A
  // household that archives its only bill and then flips the toggle still
  // needs this branch to keep rendering -- with the toggle and the archived
  // section both still on screen -- or archiving the last live bill would be
  // a dead end with no way back, the exact defect this task exists to avoid
  // one layer up.
  const noLiveBills = liveBills.length === 0;
  const noneArchived = includeArchived && data.bills.length > 0 && archivedBills.length === 0;

  // "Every bill due this month is paid": dueThisMonthMinor already sums both
  // this month's payments and this month's still-unpaid bills (the formulas
  // table's own union), so the two totals agree exactly once nothing *this
  // month* is left unpaid. Guarded on `> 0` so a month with nothing due at
  // all (state 2) does not read as "caught up" merely because 0 equals 0,
  // and on `excludedNoRate === 0` so an unpaid foreign-currency bill
  // excluded from both sums cannot make the panel claim victory while that
  // bill still sits in Due soon -- the BudgetRolloverCard precedent (commit
  // 8a1114b) for the identical shape of lie.
  //
  // None of that catches an overdue bill from a PREVIOUS month, though: an
  // overdue `next_due` contributes to neither sum (dueThisMonthMinor only
  // counts a bill whose next_due falls in the current month), so the two
  // totals can agree while an overdue row is still sitting in Due soon.
  // `nextDue` is the earliest non-null next_due over every live bill, so if
  // any live bill is overdue, the earliest one is too -- `nextDue.overdue`
  // is therefore the server's own answer to "is anything still overdue,"
  // read off the one figure already on this page rather than a second scan
  // of `liveBills`.
  const allCaughtUp =
    summary.dueThisMonthMinor > 0 &&
    summary.dueThisMonthMinor === summary.paidSoFarMinor &&
    summary.excludedNoRate === 0 &&
    summary.nextDue?.overdue !== true;

  return (
    <div data-testid="bills-page" className="flex flex-col gap-5 px-9 py-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{BILL_COPY.title}</h1>
          {summary.billCount > 0 && (
            <p data-testid="bills-subtitle" className="mt-1 text-[13px] text-muted">
              {BILL_COPY.onAutopay(summary.autopayCount, summary.billCount)}
            </p>
          )}
        </div>
        <div className="flex items-center gap-3.5">
          <div className="flex items-center gap-1.5 text-[11px] text-muted">
            <ToggleSwitch
              checked={includeArchived}
              onChange={() => setIncludeArchived((prev) => !prev)}
              label={BILL_COPY.archivedToggle}
            />
            {BILL_COPY.archivedToggle}
          </div>
          {/* Task 13's own entry point. Left unwired on purpose -- see this
              file's header comment. */}
          <button
            type="button"
            data-testid="bills-add"
            className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
          >
            {BILL_COPY.addBill}
          </button>
        </div>
      </div>

      {noneArchived && (
        <p data-testid="bills-archived-empty" className="text-xs text-muted">
          {BILL_COPY.archivedEmpty}
        </p>
      )}

      {noLiveBills ? (
        <div data-testid="bills-empty-state" className="rounded-xl border border-hairline bg-card p-16 text-center">
          <div className="text-[19px] font-semibold tracking-[-0.01em] text-ink">{BILL_COPY.emptyHeadline}</div>
          <p className="mx-auto mt-2 max-w-[420px] text-[13.5px] leading-relaxed text-muted">
            {BILL_COPY.emptyBody}
          </p>
        </div>
      ) : (
        <>
          <BillStatCards
            currency={summary.currency}
            symbol={symbolFor(summary.currency)}
            dueThisMonthMinor={summary.dueThisMonthMinor}
            paidSoFarMinor={summary.paidSoFarMinor}
            nextDue={summary.nextDue}
          />

          <div className="rounded-xl border border-hairline bg-card p-[22px]">
            <div data-testid="bills-due-soon">
              <h2 className="mb-2 text-[11px] uppercase tracking-[0.08em] text-muted">{BILL_COPY.dueSoon}</h2>
              {dueSoonBills.length === 0 ? (
                <p className="py-3 text-[13px] text-muted">{BILL_COPY.dueSoonEmpty}</p>
              ) : (
                dueSoonBills.map((bill) => (
                  <BillRow
                    key={bill.id}
                    kind="bill"
                    bill={bill}
                    symbolFor={symbolFor}
                    onArchive={handleArchive}
                    onRestore={handleRestore}
                    pending={pendingIds.has(bill.id)}
                  />
                ))
              )}
            </div>

            {/* No explanatory text when empty, unlike Due soon above -- the
                task brief pins the empty-heading failure specifically for
                Due soon (state 2's own contract line); a far-out household
                with nothing beyond 30 days just has no Later section to
                show, which is unremarkable rather than a state to explain. */}
            {laterBills.length > 0 && (
              <div data-testid="bills-later" className="mt-4">
                <h2 className="mb-2 text-[11px] uppercase tracking-[0.08em] text-muted">{BILL_COPY.later}</h2>
                {laterBills.map((bill) => (
                  <BillRow
                    key={bill.id}
                    kind="bill"
                    bill={bill}
                    symbolFor={symbolFor}
                    onArchive={handleArchive}
                    onRestore={handleRestore}
                    pending={pendingIds.has(bill.id)}
                  />
                ))}
              </div>
            )}

            {paidThisMonth.length > 0 && (
              <div data-testid="bills-paid-this-month" className="mt-4">
                <h2 className="mb-2 text-[11px] uppercase tracking-[0.08em] text-muted">
                  {BILL_COPY.paidThisMonth}
                </h2>
                {paidThisMonth.map((payment) => (
                  <BillRow key={payment.id} kind="payment" payment={payment} symbolFor={symbolFor} />
                ))}
              </div>
            )}
          </div>

          {allCaughtUp && (
            <div data-testid="bills-all-caught-up" className="rounded-xl bg-callout px-5 py-[18px]">
              <p className="text-[13px] font-semibold text-accent">{BILL_COPY.allCaughtUpHeadline}</p>
              <p className="mt-1.5 text-[12px] leading-relaxed text-accent-dark">
                {BILL_COPY.allCaughtUpBody(
                  currentMonthName(),
                  summary.nextDue
                    ? BILL_COPY.nextBillClause(summary.nextDue.billName, dayMonthLabel(summary.nextDue.dueOn))
                    : null,
                )}
              </p>
            </div>
          )}

          {summary.excludedNoRate > 0 && (
            <p data-testid="bills-excluded-no-rate" className="text-xs text-muted">
              {BILL_COPY.excludedNoRate(summary.excludedNoRate)}
            </p>
          )}
        </>
      )}

      {/* A sibling of the noLiveBills branch above, deliberately not nested
          inside it -- a household that archives its only live bill must
          still see this section (and the toggle above it) once "Show
          archived" is on, or archiving the last live bill becomes a dead
          end with no way back (Goals' own defect, docs/LEARNING.md pattern
          15, one layer up). The test named for exactly this shape --
          "archiving a household's only live bill still leaves ... the
          archived section ... reachable" -- is what pins these two blocks
          as siblings against a future edit that nests them. */}
      {includeArchived && archivedBills.length > 0 && (
        <div data-testid="bills-archived-section" className="rounded-xl border border-hairline bg-card p-[22px]">
          <h2 className="mb-2 text-[11px] uppercase tracking-[0.08em] text-muted">{BILL_COPY.archivedSection}</h2>
          {archivedBills.map((bill) => (
            <BillRow
              key={bill.id}
              kind="bill"
              bill={bill}
              symbolFor={symbolFor}
              onArchive={handleArchive}
              onRestore={handleRestore}
              pending={pendingIds.has(bill.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
