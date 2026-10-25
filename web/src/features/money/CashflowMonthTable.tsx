// One row per month: in, out, left over, kept and a light. A month that needs
// a human decision (grey) or has one on record (confirmed) gets a second line
// under it holding the reason and the button, so the same spot always answers
// "why, and what can I do about it".
//
// On a phone the in and out columns are dropped so left over and the light,
// the columns that answer "was it a good month", fit without scrolling
// sideways. The trade-off accepted: a phone shows in and out only for the
// last finished month, in the cards above the table.
import { Fragment, useState } from "react";
import {
  cashflowCopy,
  confirmationText,
  greyReasonText,
  keptCell,
} from "./cashflowCopy";
import { CashflowMonthDetail } from "./CashflowMonthDetail";
import { monthLabel } from "./month";
import type { CashflowMonth } from "./cashflowSchemas";
import { useConfirmCashflowMonth, useUnconfirmCashflowMonth } from "./useCashflow";

type Tone = "green" | "red" | "grey" | "none";

// A month's own light is deliberately plainer than the page's: it states
// whether anything was left over, and nothing about guidelines.
function rowLight(month: CashflowMonth): { tone: Tone; label: string } {
  if (month.status === "current") return { tone: "none", label: cashflowCopy.rowCurrent };
  if (month.status === "grey") return { tone: "grey", label: cashflowCopy.rowGrey };
  if (month.leftMinor <= 0) return { tone: "red", label: cashflowCopy.rowNothingLeft };
  return { tone: "green", label: cashflowCopy.rowLeftOver };
}

const DOT_CLASS: Record<Tone, string> = {
  green: "bg-accent",
  red: "bg-danger",
  grey: "bg-muted",
  none: "border border-muted bg-transparent",
};

// "Count it" clears a judgement (no income, unusually low spending). It never
// clears a missing exchange rate, which is a fact.
function canCountIt(month: CashflowMonth): boolean {
  return (
    month.status === "grey" &&
    month.confirmation === null &&
    !month.grey.includes("unconvertible") &&
    (month.grey.includes("no_income") || month.grey.includes("under_half_usual"))
  );
}

type Props = {
  months: CashflowMonth[];
  formatAmount: (minor: number) => string;
};

export function CashflowMonthTable({ months, formatAmount }: Props) {
  const [openMonth, setOpenMonth] = useState<string | null>(null);
  const confirm = useConfirmCashflowMonth();
  const unconfirm = useUnconfirmCashflowMonth();
  const busy = confirm.isPending || unconfirm.isPending;

  // Newest first: the month someone is checking is usually the latest.
  const rows = months
    .filter((month) => month.status === "solid" || month.status === "grey" || month.status === "current")
    .slice()
    .reverse();

  return (
    <div>
      {(confirm.isError || unconfirm.isError) && (
        <p role="alert" className="mb-2 text-xs text-danger">
          {cashflowCopy.confirmError}
        </p>
      )}
      {/* The padding, given back by the negative margin, leaves room for the
          focus ring: overflow-x-auto clips anything outside its box, which cut
          the ring off the month names, Close and the link on the left edge. */}
      <div className="-mx-1.5 overflow-x-auto px-1.5">
        <table data-testid="cashflow-table" className="w-full border-collapse text-[13px] sm:min-w-[520px]">
          <thead>
            <tr className="text-left text-xs text-muted">
              <th scope="col" className="py-2 pr-2 font-normal sm:pr-3">{cashflowCopy.tableMonth}</th>
              <th scope="col" className="hidden py-2 pr-3 text-right font-normal sm:table-cell">{cashflowCopy.moneyIn}</th>
              <th scope="col" className="hidden py-2 pr-3 text-right font-normal sm:table-cell">{cashflowCopy.moneyOut}</th>
              <th scope="col" className="py-2 pr-2 text-right font-normal sm:pr-3">{cashflowCopy.leftOver}</th>
              <th scope="col" className="py-2 pr-2 text-right font-normal sm:pr-3">{cashflowCopy.tableKept}</th>
              <th scope="col" className="py-2 font-normal">{cashflowCopy.tableLight}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((month) => {
              const light = rowLight(month);
              const open = openMonth === month.month;
              const showNote = month.confirmation !== null || month.status === "grey";
              return (
                <Fragment key={month.month}>
                  <tr className="border-t border-hairline">
                    <td className="py-2 pr-2 sm:pr-3">
                      <button
                        type="button"
                        aria-expanded={open}
                        aria-controls={open ? `cashflow-detail-${month.month}` : undefined}
                        onClick={() => setOpenMonth(open ? null : month.month)}
                        className="min-h-11 text-left font-semibold text-accent hover:underline lg:min-h-0"
                      >
                        {monthLabel(month.month)}
                      </button>
                    </td>
                    <td className="tabular hidden py-2 pr-3 text-right text-ink sm:table-cell">{formatAmount(month.inMinor)}</td>
                    <td className="tabular hidden py-2 pr-3 text-right text-ink sm:table-cell">{formatAmount(month.outMinor)}</td>
                    <td className="tabular py-2 pr-2 text-right text-ink sm:pr-3">{formatAmount(month.leftMinor)}</td>
                    <td className="tabular py-2 pr-2 text-right text-ink sm:pr-3">{keptCell(month.keptBasisPoints)}</td>
                    <td className="py-2" data-tone={light.tone}>
                      <span className="inline-flex items-center gap-1.5 text-xs text-ink">
                        <span aria-hidden="true" className={`inline-block h-2.5 w-2.5 shrink-0 rounded-full ${DOT_CLASS[light.tone]}`} />
                        {light.label}
                      </span>
                    </td>
                  </tr>
                  {showNote && (
                    <tr>
                      <td colSpan={6} className="pb-2">
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
                          {/* A confirmation never clears a missing exchange rate, so a
                              counted month can still be grey: say why beside who counted it. */}
                          {month.status === "grey" &&
                            month.grey.map((reason) => (
                              <span key={reason}>{greyReasonText(reason, month.unconvertibleCurrencies)}</span>
                            ))}
                          {month.confirmation !== null ? (
                            <>
                              <span>{confirmationText(month.confirmation)}</span>
                              <button
                                type="button"
                                disabled={busy}
                                onClick={() => unconfirm.mutate(month.month)}
                                className="min-h-11 font-semibold text-accent disabled:opacity-60 lg:min-h-0"
                              >
                                {cashflowCopy.undo}
                              </button>
                            </>
                          ) : (
                            canCountIt(month) && (
                              <button
                                type="button"
                                disabled={busy}
                                onClick={() => confirm.mutate(month.month)}
                                className="min-h-11 font-semibold text-accent disabled:opacity-60 lg:min-h-0"
                              >
                                {cashflowCopy.countIt}
                              </button>
                            )
                          )}
                        </div>
                      </td>
                    </tr>
                  )}
                  {open && (
                    <tr>
                      <td id={`cashflow-detail-${month.month}`} colSpan={6}>
                        <CashflowMonthDetail month={month} formatAmount={formatAmount} onClose={() => setOpenMonth(null)} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
