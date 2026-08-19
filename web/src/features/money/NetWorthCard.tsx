// The headline figure. Rendered as a <section role="region"> with its own
// accessible name (an aria-labelledby, not a bare <section>, which the ARIA
// spec only exposes as the "region" role once it has one) so a card that
// repeats the same figure as another part of the page -- a single-account
// household's net worth equals that account's own balance -- can still be
// addressed unambiguously in a test or by assistive tech.
import type { ReactNode } from "react";
import { useCurrencies } from "../auth/useAuth";
import { FINANCES_COPY } from "./copy";
import { formatMoney } from "./formatMoney";
import type { Summary } from "./schemas";

// chart is a slot rather than something this card decides for itself: the
// design draws the bars on Finances and not on Overview, and both screens
// mount this same card. changeNote is the one word of copy that differs
// between them -- Overview's card says "this month", Finances' figure sits
// beside its own "Last 12 months" heading and does not need it.
export function NetWorthCard({
  summary,
  chart,
  changeNote,
}: {
  summary: Summary;
  chart?: ReactNode;
  changeNote?: string;
}) {
  const currencies = useCurrencies();
  const symbol = currencies.data?.currencies.find(
    (c) => c.code === summary.currency,
  )?.symbol;

  if (!summary.computable) {
    // considered > 0 && converted === 0 (usecase/networth.go): every
    // non-archived account is in excludedNoRate here, so the distinct
    // currencies in it are exactly what the household's own currency has no
    // rate for. No figure is shown -- there is no honest number to show.
    const others = [...new Set(summary.excludedNoRate.map((e) => e.currency))].join(", ");
    return (
      <section
        aria-labelledby="net-worth-heading"
        className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
      >
        <h2 id="net-worth-heading" className="text-xs text-muted">
          {FINANCES_COPY.netWorth}
        </h2>
        <p className="mt-3 text-[13px] text-ink">
          {FINANCES_COPY.notComputable(summary.currency, others)}
        </p>
      </section>
    );
  }

  return (
    <section
      aria-labelledby="net-worth-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-baseline justify-between">
        <h2 id="net-worth-heading" className="text-xs text-muted">
          {FINANCES_COPY.netWorth}
        </h2>
        {chart && <span className="text-xs text-muted">{FINANCES_COPY.trendWindow}</span>}
      </div>
      <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
        {formatMoney(summary.netWorthMinor, summary.currency, symbol)}
        {summary.trend?.changeBasisPoints !== undefined && (
          <span
            className={`ml-2 text-[13px] font-semibold tracking-normal ${
              summary.trend.changeBasisPoints < 0 ? "text-danger" : "text-accent"
            }`}
          >
            {FINANCES_COPY.trendChange(summary.trend.changeBasisPoints)}
            {changeNote ? ` ${changeNote}` : ""}
          </span>
        )}
      </p>
      {chart}

      {summary.excludedNoRate.length > 0 && (
        <p className="mt-3 text-[11.5px] text-muted">
          {FINANCES_COPY.excludedNoRate(
            summary.excludedNoRate.length,
            [...new Set(summary.excludedNoRate.map((e) => e.currency))].join(", "),
          )}
        </p>
      )}
      {summary.excludedByChoice > 0 && (
        <p className="mt-1 text-[11.5px] text-muted">
          {FINANCES_COPY.excludedByChoice(summary.excludedByChoice)}
        </p>
      )}
    </section>
  );
}
