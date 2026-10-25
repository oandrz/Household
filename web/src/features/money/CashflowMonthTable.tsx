// One row per month: in, out, left over, kept and a light. A month that needs
// a human decision (grey) or has one on record (confirmed) gets a second line
// under it holding the reason and the button, so the same spot always answers
// "why, and what can I do about it".
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
      <div className="overflow-x-auto">
        <table data-testid="cashflow-table" className="w-full min-w-[520px] border-collapse text-[13px]">
          <thead>
            <tr className="text-left text-xs text-muted">
              <th scope="col" className="py-2 pr-3 font-normal">{cashflowCopy.tableMonth}</th>
              <th scope="col" className="py-2 pr-3 text-right font-normal">{cashflowCopy.moneyIn}</th>
              <th scope="col" className="py-2 pr-3 text-right font-normal">{cashflowCopy.moneyOut}</th>
              <th scope="col" className="py-2 pr-3 text-right font-normal">{cashflowCopy.leftOver}</th>
              <th scope="col" className="py-2 pr-3 text-right font-normal">{cashflowCopy.tableKept}</th>
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
                    <td className="py-2 pr-3">
                      <button
                        type="button"
                        aria-expanded={open}
                        onClick={() => setOpenMonth(open ? null : month.month)}
                        className="min-h-11 text-left font-semibold text-accent lg:min-h-0"
                      >
                        {monthLabel(month.month)}
                      </button>
                    </td>
                    <td className="py-2 pr-3 text-right text-ink">{formatAmount(month.inMinor)}</td>
                    <td className="py-2 pr-3 text-right text-ink">{formatAmount(month.outMinor)}</td>
                    <td className="py-2 pr-3 text-right text-ink">{formatAmount(month.leftMinor)}</td>
                    <td className="py-2 pr-3 text-right text-ink">{keptCell(month.keptBasisPoints)}</td>
                    <td className="py-2" data-tone={light.tone}>
                      <span className="inline-flex items-center gap-1.5 text-xs text-ink">
                        <span aria-hidden="true" className={`inline-block h-2.5 w-2.5 rounded-full ${DOT_CLASS[light.tone]}`} />
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
                      <td colSpan={6}>
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
