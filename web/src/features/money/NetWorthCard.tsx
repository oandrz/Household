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
// beside its own "Last 12 months" heading and does not need it. changeNote's
// presence also decides the change's own layout, not just its wording: with
// no changeNote (Finances) the bare "▲ 2.1%" stays inline beside the figure,
// matching the design's Finances screen; with one (Overview) the whole
// change renders as its own line underneath, matching the design's Overview
// tile -- a bare arrow-and-percentage fits inline, "▲ 2.1% this month"
// measured wider than an ordinary phone's content column and wrapped
// mid-phrase when it was kept inline (docs/LEARNING.md pattern 3).
//
// chartEmptyNote is separate from chart, not a second thing packed into it:
// Finances passes it instead of chart when there's a trend but not enough of
// one to draw (FinancesPage.tsx's own hasDrawableTrend call), so the note
// renders inside this card's own padding -- the same convention
// BreakdownCard, RecentTransactionsCard and this card's own not-computable
// branch below all follow for their explanatory text -- while the "Last 12
// months" heading, still gated on `chart` alone, stays suppressed. Passing a
// JSX element through `chart` for this case instead (as an earlier version
// of this fix did) made `chart` truthy for content that wasn't a chart,
// which is exactly what re-triggered the heading.
export function NetWorthCard({
  summary,
  chart,
  chartEmptyNote,
  changeNote,
}: {
  summary: Summary;
  chart?: ReactNode;
  chartEmptyNote?: string;
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

  // Hoisted out of the two render sites below (inline span, block <p>) so
  // the arrow-and-percentage text and its colour are computed once, not
  // duplicated between them and left free to drift apart.
  const changeText =
    summary.trend?.changeBasisPoints !== undefined
      ? FINANCES_COPY.trendChange(summary.trend.changeBasisPoints)
      : null;
  const changeColorClass =
    summary.trend?.changeBasisPoints !== undefined && summary.trend.changeBasisPoints < 0
      ? "text-danger"
      : "text-accent";

  return (
    <section
      aria-labelledby="net-worth-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-baseline justify-between">
        <h2 id="net-worth-heading" className="text-xs text-muted">
          {FINANCES_COPY.netWorth}
        </h2>
        {/* A JSX element is truthy regardless of what it renders, so this
            label must never be gated on a chart that merely exists -- only
            on one FinancesPage.tsx has already decided (via
            NetWorthChart.tsx's own hasDrawableTrend) is worth naming
            "Last 12 months". FinancesPage passes `null`, not the chart's own
            empty-state text, for exactly that reason: this line trusts the
            caller to have made that call already, rather than re-deriving
            it here. */}
        {chart && <span className="text-xs text-muted">{FINANCES_COPY.trendWindow}</span>}
      </div>
      <p className="tabular mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
        {formatMoney(summary.netWorthMinor, summary.currency, symbol)}
        {/* Finances passes no changeNote, so its own bare "▲ 2.1%" -- no
            trailing words -- stays inline beside the figure, matching the
            design's Finances screen, which sits the percentage right next to
            "Last 12 months". Overview's copy adds "this month", which is
            exactly what wrapped mid-phrase at 360px/320px when this stayed
            inline (docs/LEARNING.md pattern 3) -- so once changeNote is
            given, the whole change renders as its own line under the figure
            instead (below), matching the design's own Overview card
            (design/Household Dashboard.dc.html, the net worth tile), which
            never puts the two on one line either. */}
        {changeText !== null && !changeNote && (
          <span
            data-testid="net-worth-change"
            // normal-nums overrides the inherited tabular-nums from the
            // figure's own <p>: this badge is a single fluctuating
            // percentage, not a column of figures to line up, so fixed
            // digit widths buy it nothing and it reads better proportional.
            className={`ml-2 text-[13px] font-semibold tracking-normal normal-nums ${changeColorClass}`}
          >
            {changeText}
          </span>
        )}
      </p>
      {changeText !== null && changeNote && (
        <p
          data-testid="net-worth-change"
          className={`mt-1 text-[13px] font-semibold tracking-normal ${changeColorClass}`}
        >
          {changeText} {changeNote}
        </p>
      )}
      {chart}
      {/* Same slot the chart itself would occupy (mt-4 text-[12.5px]
          text-muted matches NetWorthChart.tsx's own trendEmpty paragraph
          exactly), so a household reads the same words in the same place
          whether the "not enough history" branch is this one or that
          component's -- only which of the two rendered it differs. */}
      {chartEmptyNote && <p className="mt-4 text-[12.5px] text-muted">{chartEmptyNote}</p>}

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
