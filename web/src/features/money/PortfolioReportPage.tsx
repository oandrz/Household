// The period report: what each holding earned over a quarter, a half-year or a
// year, split into the three components the PRD pins, with a bar chart above
// the table comparing those periods over time.
//
// Composition only -- fetch orchestration lives in usePortfolioReport.ts, the
// GoalsPage.tsx/useGoals.ts convention.
//
// Four things here are rendered from what the SERVER said rather than from
// logic invented on this side:
//
//   periods          including how many of them. The window is the server's
//                    choice per period kind, because the chart's bar budget is
//                    what those numbers were chosen against.
//   current          the period still running is labelled "to date". A
//                    half-finished quarter is not a result.
//   unrealised/total null means the figure cannot be known, and `reason` says
//                    which price was missing. Never rendered as a zero: a
//                    quarter nobody priced is unknowable, not flat.
//   closingPriceAsOf the day the figure was measured, so the page can say how
//                    old it is. Valuations going quietly stale is this
//                    feature's largest product risk.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { useCurrencies } from "../auth/useAuth";
import { formatMoney } from "./formatMoney";
import { HOLDING_REPORT_COPY, blankReasonCopy, periodHeading, priceAgeLabel } from "./holdingReportCopy";
import { PeriodReturnChart } from "./PeriodReturnChart";
import { usePortfolioReport } from "./usePortfolioReport";
import type { PeriodKind, PeriodReturn, ReportHolding, ReportPeriod } from "./holdingSchemas";

const KINDS: PeriodKind[] = ["quarter", "half", "year"];

// Local time, never toISOString(): that renders in UTC, so for the eight hours
// a day this household is ahead of it every date would read as yesterday.
// AccountModal.tsx and HoldingLotsPanel.tsx each carry their own copy of this
// for the same reason -- three features' date handling deliberately not
// coupled through one import.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function PortfolioReportPage() {
  const [kind, setKind] = useState<PeriodKind>("quarter");
  const report = usePortfolioReport({ kind });
  const currencies = useCurrencies();
  // Every figure goes through the household's own symbol table; without it
  // formatMoney falls back to the bare code ("SGD 240.00"), which is not what
  // any other money screen shows.
  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;

  const periods = report.data?.periods ?? [];
  const holdings = report.data?.holdings ?? [];

  return (
    <PageContainer>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
            {HOLDING_REPORT_COPY.title}
          </h1>
          <p className="mt-1 text-[13px] text-muted">
            Profit and loss per holding, split into what it is worth, what you
            sold, and what it paid you.
          </p>
        </div>
        <Link
          to="/money/portfolio"
          className="min-h-11 rounded-lg border border-hairline px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0"
        >
          Back to portfolio
        </Link>
      </header>

      <p
        data-testid="report-not-in-net-worth"
        className="mt-5 rounded-xl border border-hairline bg-surface px-4 py-3 text-[12.5px] leading-snug text-muted"
      >
        {HOLDING_REPORT_COPY.notInNetWorth}
      </p>

      <div role="group" aria-label="Period length" className="mt-5 flex gap-1.5">
        {KINDS.map((option) => (
          <button
            key={option}
            type="button"
            aria-pressed={kind === option}
            onClick={() => setKind(option)}
            className={
              kind === option
                ? "min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0"
                : "min-h-11 rounded-lg border border-hairline px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0"
            }
          >
            {HOLDING_REPORT_COPY.periodKinds[option]}
          </button>
        ))}
      </div>

      {report.isPending ? <p className="mt-5 text-xs text-muted">Working out the figures…</p> : null}
      {report.isError ? (
        <p className="mt-5 text-xs text-danger" role="alert">
          The report could not be loaded. Try again in a moment.
        </p>
      ) : null}

      {report.isSuccess && holdings.length === 0 ? (
        <div className="mt-5 rounded-xl border border-hairline bg-card p-[22px]">
          <h2 className="text-[15px] font-semibold text-ink">Nothing to report yet</h2>
          <p className="mt-1.5 text-[13px] text-muted">{HOLDING_REPORT_COPY.empty}</p>
        </div>
      ) : null}

      {report.isSuccess && holdings.length > 0 ? (
        <>
          <section className="mt-5 rounded-xl border border-hairline bg-card p-[22px]">
            <h2 className="text-[15px] font-semibold text-ink">
              {HOLDING_REPORT_COPY.periodKinds[kind]} by {kind === "year" ? "year" : "period"}
            </h2>
            <PeriodReturnChart
              periods={periods}
              holdings={holdings}
              primaryCurrency={report.data.primaryCurrency}
            />
          </section>

          <div className="mt-5 flex flex-col gap-3">
            {holdings.map((holding) => (
              <HoldingReportCard
                key={holding.id}
                holding={holding}
                periods={periods}
                primaryCurrency={report.data.primaryCurrency}
                symbolFor={symbolFor}
              />
            ))}
          </div>

          <p className="mt-3 text-[11.5px] text-muted">{HOLDING_REPORT_COPY.feesNote}</p>
        </>
      ) : null}
    </PageContainer>
  );
}

function HoldingReportCard({
  holding,
  periods,
  primaryCurrency,
  symbolFor,
}: {
  holding: ReportHolding;
  periods: ReportPeriod[];
  primaryCurrency: string;
  symbolFor: (currency: string) => string | undefined;
}) {
  return (
    <section
      data-testid="report-holding"
      data-holding={holding.name}
      className="rounded-xl border border-hairline bg-card p-[22px]"
    >
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-[15px] font-semibold text-ink">
          {holding.name}
          {holding.archived ? <span className="ml-2 text-[11.5px] text-muted">Archived</span> : null}
        </h3>
        <p className="text-[11.5px] text-muted">
          {holding.accountName} · priced in {holding.currency}
        </p>
      </header>

      <div className="mt-3 overflow-x-auto">
        <table className="w-full min-w-[34rem] border-collapse text-[12.5px]">
          <thead>
            <tr className="text-left text-muted">
              <th scope="col" className="py-1.5 pr-3 font-medium">Period</th>
              <th scope="col" className="py-1.5 pr-3 text-right font-medium">{HOLDING_REPORT_COPY.unrealised}</th>
              <th scope="col" className="py-1.5 pr-3 text-right font-medium">{HOLDING_REPORT_COPY.realised}</th>
              <th scope="col" className="py-1.5 pr-3 text-right font-medium">{HOLDING_REPORT_COPY.income}</th>
              <th scope="col" className="py-1.5 pr-3 text-right font-medium">{HOLDING_REPORT_COPY.fees}</th>
              <th scope="col" className="py-1.5 text-right font-medium">{HOLDING_REPORT_COPY.total}</th>
            </tr>
          </thead>
          <tbody>
            {/* Newest first here, the reverse of the chart's oldest-to-newest
                axis: a table is read from the top and the period the owner is
                in is the one they came for, while a chart is read left to
                right through time. */}
            {[...periods].reverse().map((period, reversedIndex) => {
              const figures = holding.returns[periods.length - 1 - reversedIndex];
              if (!figures) return null;
              return (
                <ReportRow
                  key={period.label}
                  period={period}
                  figures={figures}
                  holdingCurrency={holding.currency}
                  primaryCurrency={primaryCurrency}
                  symbolFor={symbolFor}
                />
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function ReportRow({
  period,
  figures,
  holdingCurrency,
  primaryCurrency,
  symbolFor,
}: {
  period: ReportPeriod;
  figures: PeriodReturn;
  holdingCurrency: string;
  primaryCurrency: string;
  symbolFor: (currency: string) => string | undefined;
}) {
  // The native figure is shown beside the primary one only when they are
  // genuinely two different numbers. For a holding already in the household's
  // currency they would be the same figure printed twice.
  const showNative = holdingCurrency !== primaryCurrency;
  const money = (minor: number, currency: string) =>
    formatMoney(minor, currency, symbolFor(currency));

  const cell = (component: { nativeMinor: number; primaryMinor: number } | null) => {
    if (!component) return <span className="text-muted">—</span>;
    return (
      <>
        {money(component.primaryMinor, primaryCurrency)}
        {showNative ? (
          <span className="block text-[11px] text-muted">
            {money(component.nativeMinor, holdingCurrency)}
          </span>
        ) : null}
      </>
    );
  };

  const age = priceAgeLabel(figures.closingPriceAsOf, today());

  return (
    <tr data-testid="report-row" data-period={period.label} className="border-t border-hairline align-top">
      <th scope="row" className="py-2 pr-3 text-left font-normal text-ink">
        {periodHeading(period)}
        {age ? <span className="block text-[11px] text-muted">{age}</span> : null}
        {figures.reason ? (
          <span data-testid="report-blank-reason" className="block text-[11px] text-muted">
            {blankReasonCopy(figures.reason)}
          </span>
        ) : null}
      </th>
      <td className="py-2 pr-3 text-right text-ink">{cell(figures.unrealised)}</td>
      <td className="py-2 pr-3 text-right text-ink">{cell(figures.realised)}</td>
      <td className="py-2 pr-3 text-right text-ink">{cell(figures.income)}</td>
      <td className="py-2 pr-3 text-right text-ink">{cell(figures.fees)}</td>
      <td className="py-2 text-right font-semibold text-ink">{cell(figures.total)}</td>
    </tr>
  );
}
