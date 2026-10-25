// The Yearly tab: the light, kept so far (against the same months last year,
// never last year's full total), heading for (this year only), the cash
// cushion, the chart and the bills still to come.
import {
  cashflowCopy,
  cushionReasonText,
  cushionText,
  dateLabel,
  percentText,
} from "./cashflowCopy";
import { CashflowChart } from "./CashflowChart";
import { CashflowFigure } from "./CashflowFigure";
import { CashflowLight } from "./CashflowLight";
import type { CashflowPage } from "./cashflowSchemas";

type Props = {
  data: CashflowPage;
  formatAmount: (minor: number) => string;
  onSelectYear: (year: number) => void;
};

export function CashflowYearly({ data, formatAmount, onSelectYear }: Props) {
  const { year, cushion } = data;
  const currentYear = Number(data.current.month.slice(0, 4));
  const canGoBack = year.year > data.oldestYear;
  const canGoForward = year.year < currentYear;
  // With one year of data there is nowhere to step to, so the arrows would
  // only be two dead buttons.
  const onlyOneYear = data.oldestYear === 0 || data.oldestYear === currentYear;
  const pickerButton = "min-h-11 min-w-11 rounded-lg border border-hairline text-[13px] text-ink transition-colors duration-[var(--transition-state)] enabled:hover:bg-canvas enabled:active:bg-toggle-off disabled:opacity-40 lg:min-h-0 lg:min-w-0 lg:px-2.5 lg:py-1";

  return (
    <div className="flex flex-col gap-5">
      <CashflowLight light={data.light} averageMonths={data.average.months} />

      <div className="flex items-center gap-2">
        {!onlyOneYear && (
          <button
            type="button"
            aria-label={cashflowCopy.previousYear}
            disabled={!canGoBack}
            onClick={() => onSelectYear(year.year - 1)}
            className={pickerButton}
          >
            ◀
          </button>
        )}
        <span data-testid="cashflow-year" className="min-w-14 text-center text-[15px] font-semibold text-ink">
          {year.year}
        </span>
        {!onlyOneYear && (
          <button
            type="button"
            aria-label={cashflowCopy.nextYear}
            disabled={!canGoForward}
            onClick={() => onSelectYear(year.year + 1)}
            className={pickerButton}
          >
            ▶
          </button>
        )}
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <CashflowFigure label={cashflowCopy.keptSoFar} value={formatAmount(year.keptSoFarMinor)}>
          {year.keptBasisPoints !== null && <span>{cashflowCopy.shareKept(percentText(year.keptBasisPoints))}</span>}
          <span>
            {year.sameMonthsLastYearMinor === null
              ? cashflowCopy.noLastYear
              : cashflowCopy.sameMonthsLastYear(formatAmount(year.sameMonthsLastYearMinor))}
          </span>
          {year.greyMonths > 0 && <span>{cashflowCopy.greyMonths(year.greyMonths)}</span>}
        </CashflowFigure>
        {year.headingForMinor !== null && (
          <CashflowFigure label={cashflowCopy.headingFor} value={formatAmount(year.headingForMinor)}>
            <span>{cashflowCopy.headingForNote(data.average.months)}</span>
          </CashflowFigure>
        )}
        <CashflowFigure label={cashflowCopy.cashCushion} value={cushionText(cushion)}>
          <span>{cushionReasonText(cushion)}</span>
        </CashflowFigure>
      </div>

      <section className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 className="mb-3 text-xs text-muted">{cashflowCopy.chartLabel}</h2>
        <CashflowChart months={year.months} currency={data.currency} formatAmount={formatAmount} />
      </section>

      <section className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 className="text-xs text-muted">{cashflowCopy.billsToCome}</h2>
        <p className="mt-1 text-[11.5px] text-muted">{cashflowCopy.billsToComeNote}</p>
        {year.billsToCome.length === 0 ? (
          <p className="mt-3 text-[13px] text-muted">{cashflowCopy.billsNone}</p>
        ) : (
          <ul className="mt-3 flex flex-col gap-2">
            {year.billsToCome.map((bill) => (
              <li key={bill.billId} className="flex items-baseline justify-between gap-3 text-[13px]">
                <span className="min-w-0">
                  <span className="text-ink">{bill.name}</span>
                  <span className="block text-[11.5px] text-muted">{dateLabel(bill.nextDue)}</span>
                </span>
                <span className="tabular shrink-0 text-ink">
                  {bill.unconvertible ? cashflowCopy.billUnconvertible : formatAmount(bill.amountMinor)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
