// The Monthly tab: the light, the last finished month's three figures, the
// 3-month average, the month in progress (no colour: it is not a result yet),
// the chart and the month-by-month table.
import { cashflowCopy, greyReasonText, keptLine, lastFinishedLabel } from "./cashflowCopy";
import { CashflowChart } from "./CashflowChart";
import { CashflowFigure } from "./CashflowFigure";
import { CashflowLight } from "./CashflowLight";
import { CashflowMonthTable } from "./CashflowMonthTable";
import { monthLabel } from "./month";
import type { CashflowPage } from "./cashflowSchemas";

type Props = {
  data: CashflowPage;
  formatAmount: (minor: number) => string;
};

export function CashflowMonthly({ data, formatAmount }: Props) {
  const { lastFinished, average, current } = data;
  return (
    <div className="flex flex-col gap-5">
      <CashflowLight light={data.light} averageMonths={average.months} />

      {lastFinished === null ? (
        <p className="text-[13px] text-muted">{cashflowCopy.noFinishedMonth}</p>
      ) : (
        <section aria-labelledby="cashflow-last-month" className="flex flex-col gap-3">
          <h2 id="cashflow-last-month" className="text-[13px] font-semibold text-ink">
            {lastFinishedLabel(lastFinished.month)}
          </h2>
          {lastFinished.status === "grey" && (
            <div data-testid="cashflow-last-grey" className="rounded-lg border border-hairline bg-card p-3 text-xs text-muted">
              <div className="font-semibold text-ink">{cashflowCopy.rowGrey}</div>
              {lastFinished.grey.map((reason) => (
                <div key={reason}>{greyReasonText(reason, lastFinished.unconvertibleCurrencies)}</div>
              ))}
            </div>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <CashflowFigure
              testId="cashflow-figure-in"
              label={cashflowCopy.moneyIn}
              value={formatAmount(lastFinished.inMinor)}
            />
            <CashflowFigure
              testId="cashflow-figure-out"
              label={cashflowCopy.moneyOut}
              value={formatAmount(lastFinished.outMinor)}
            >
              {lastFinished.spreadMinor !== 0 && (
                <span>{cashflowCopy.spreadNote(formatAmount(lastFinished.spreadMinor))}</span>
              )}
              {lastFinished.loanMinor !== 0 && (
                <span>{cashflowCopy.loanNote(formatAmount(lastFinished.loanMinor))}</span>
              )}
            </CashflowFigure>
            <CashflowFigure
              testId="cashflow-figure-left"
              label={cashflowCopy.leftOver}
              value={formatAmount(lastFinished.leftMinor)}
            >
              <span>{keptLine(lastFinished.keptBasisPoints)}</span>
            </CashflowFigure>
          </div>
        </section>
      )}

      <section className="rounded-xl border border-hairline bg-card p-[22px] text-[13px]">
        {average.months === 0 ? (
          <p className="text-muted">{cashflowCopy.noAverage}</p>
        ) : (
          <>
            <div className="text-xs text-muted">
              {average.months < 3 ? cashflowCopy.averageOf(average.months) : cashflowCopy.average}
            </div>
            {/* Each label stays on one line with its amount: a phone would
                otherwise break "Left over" from the figure it names. */}
            <div className="mt-1 text-ink">
              <span className="whitespace-nowrap">{cashflowCopy.moneyIn} {formatAmount(average.inMinor)}</span> ·{" "}
              <span className="whitespace-nowrap">{cashflowCopy.moneyOut} {formatAmount(average.outMinor)}</span> ·{" "}
              <span className="whitespace-nowrap">{cashflowCopy.leftOver} {formatAmount(average.leftMinor)}</span> ·{" "}
              <span className="whitespace-nowrap">{keptLine(average.keptBasisPoints)}</span>
            </div>
          </>
        )}
      </section>

      {/* No colour and no light: a month in progress looks wonderful on the
          25th, before the rest of its spending has been entered. */}
      <section
        data-testid="cashflow-current"
        data-tone="none"
        className="rounded-xl border border-dashed border-hairline bg-card p-[22px] text-[13px]"
      >
        <div className="text-xs text-muted">{cashflowCopy.soFar(monthLabel(current.month))}</div>
        <div className="mt-1 text-ink">
          <span className="whitespace-nowrap">{cashflowCopy.moneyIn} {formatAmount(current.inMinor)}</span> ·{" "}
          <span className="whitespace-nowrap">{cashflowCopy.moneyOut} {formatAmount(current.outMinor)}</span> ·{" "}
          <span className="whitespace-nowrap">{cashflowCopy.leftOver} {formatAmount(current.leftMinor)}</span>
        </div>
        <div className="mt-1 text-xs text-muted">{cashflowCopy.soFarNote}</div>
      </section>

      <section className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 className="mb-3 text-xs text-muted">{cashflowCopy.chartLabel}</h2>
        <CashflowChart months={data.recent} currency={data.currency} formatAmount={formatAmount} />
      </section>

      <section className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 className="mb-3 text-xs text-muted">{cashflowCopy.tableHeading}</h2>
        <CashflowMonthTable months={data.recent} formatAmount={formatAmount} />
      </section>
    </div>
  );
}
