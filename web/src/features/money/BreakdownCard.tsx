// The assets-and-liabilities list: one row per type that actually has
// money in it, plus a Net row underneath. Renders nothing at all when the
// summary is incomputable -- usecase/networth.go's Summary only ever adds to
// byType (the map this card's rows come from) *after* a successful convert,
// so an incomputable summary's breakdown is provably empty and its
// netWorthMinor does not exist on the wire to put in the Net row. NetWorthCard
// is where that state gets explained; this card has nothing to add to it.
import { useCurrencies } from "../auth/useAuth";
import { ACCOUNT_TYPE_LABELS, LIABILITY_TYPES } from "./accountTypes";
import { FINANCES_COPY } from "./copy";
import { formatMoney } from "./formatMoney";
import type { Summary } from "./schemas";

export function BreakdownCard({ summary }: { summary: Summary }) {
  const currencies = useCurrencies();

  if (!summary.computable) return null;

  const symbol = currencies.data?.currencies.find(
    (c) => c.code === summary.currency,
  )?.symbol;

  return (
    <section
      aria-labelledby="breakdown-heading"
      className="flex flex-col gap-4 rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="breakdown-heading" className="text-sm font-semibold text-ink">
        {FINANCES_COPY.assetsAndLiabilities}
      </h2>

      <div className="flex flex-col gap-2.5 text-[13px]">
        {summary.breakdown.map((entry) => {
          // The backend stores every type's total unsigned (a debt is never a
          // negative number in the database); the minus sign belongs to the
          // screen, not the row, so it is applied here from LIABILITY_TYPES
          // rather than trusted from the wire.
          const signedMinor = LIABILITY_TYPES.has(entry.type)
            ? -entry.totalMinor
            : entry.totalMinor;
          return (
            <div key={entry.type} className="flex items-center justify-between">
              <span className="text-ink">{ACCOUNT_TYPE_LABELS[entry.type]}</span>
              <span className="font-semibold text-ink">
                {formatMoney(signedMinor, summary.currency, symbol)}
              </span>
            </div>
          );
        })}
      </div>

      <div className="flex items-center justify-between border-t border-hairline pt-3 text-[13px]">
        <span className="text-muted">{FINANCES_COPY.net}</span>
        <span className="font-semibold text-ink">
          {formatMoney(summary.netWorthMinor, summary.currency, symbol)}
        </span>
      </div>

      {summary.excludedByChoice > 0 && (
        <p className="text-[11.5px] text-muted">{FINANCES_COPY.breakdownFootnote}</p>
      )}
    </section>
  );
}
