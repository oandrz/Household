// The Portfolio screen: what the household owns, what it cost, and what it is
// worth. Composition only -- fetch orchestration lives in useHoldings.ts, the
// GoalsPage.tsx/useGoals.ts convention.
//
// Three things on this page are rendered from what the SERVER said, never from
// logic invented here:
//
//   notInNetWorth   the banner. Milestone 1 keeps holdings out of net worth and
//                   the twelve-month trend, and the page says so on its face.
//                   When that changes it changes on the server and this
//                   follows, with no edit here.
//   hasMarketValue  false means NO figure, not a figure of zero. A holding
//                   nobody has priced is unknowable, not worthless -- the same
//                   "blank it and say why" rule the net worth card follows when
//                   a primary-currency change strands an account.
//   valuedAt        how stale that price is. Valuations going quietly stale is
//                   this feature's largest product risk, so the age is shown
//                   beside the figure rather than being available on request.
import { useState } from "react";
import { useCurrencies } from "../auth/useAuth";
import { PageContainer } from "../../components/PageContainer";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { formatMoney } from "./formatMoney";
import { HoldingModal } from "./HoldingModal";
import { HoldingLotsPanel } from "./HoldingLotsPanel";
import { useHoldings } from "./useHoldings";
import type { Holding } from "./holdingSchemas";

const INSTRUMENT_LABEL: Record<Holding["instrument"], string> = {
  stock: "Stock",
  gold: "Gold",
  other: "Other",
};

export function PortfolioPage() {
  const [includeArchived, setIncludeArchived] = useState(false);
  const holdings = useHoldings({ includeArchived });
  const currencies = useCurrencies();
  // Every money figure on this page goes through the household's own symbol
  // table. Without it formatMoney falls back to the bare code ("SGD 26,000.00"),
  // which is not what any other money screen shows -- BillsPage.tsx's own
  // symbolFor, restated.
  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;
  const [modalHolding, setModalHolding] = useState<Holding | "new" | null>(null);
  // A separate state slot from modalHolding rather than a union sharing it:
  // the two surfaces can never be open for the same click, and keeping them
  // independent means a change to one never has to reason about the other --
  // GoalsPage.tsx's own reasoning for the same pair.
  const [lotsHolding, setLotsHolding] = useState<Holding | null>(null);
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());

  const markPending = (id: string, pending: boolean) => {
    setPendingIds((current) => {
      const next = new Set(current);
      if (pending) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const rows = holdings.data?.holdings ?? [];

  return (
    <PageContainer>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">Portfolio</h1>
          <p className="mt-1 text-[13px] text-muted">
            What you hold, what it cost, and what it is worth.
          </p>
        </div>
        <button type="button" className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0" onClick={() => setModalHolding("new")}>
          Add holding
        </button>
      </header>

      {/* Gated on isSuccess as well as the flag, so the banner does not appear
          a beat after the page does -- it is either shown with the figures it
          qualifies, or not at all. */}
      {holdings.isSuccess && holdings.data.notInNetWorth ? (
        <p className="mt-5 rounded-xl border border-hairline bg-surface px-4 py-3 text-[12.5px] leading-snug text-muted" data-testid="not-in-net-worth">
          Holdings are not counted in your net worth yet. The figures here stand
          on their own.
        </p>
      ) : null}

      <div className="mt-5 flex items-center gap-1.5 text-[11px] text-muted">
        <ToggleSwitch
          label="Show archived"
          checked={includeArchived}
          onChange={() => setIncludeArchived((on) => !on)}
        />
      </div>

      {holdings.isPending ? <p className="mt-5 text-xs text-muted">Loading your holdings…</p> : null}
      {holdings.isError ? (
        <p className="mt-5 text-xs text-danger" role="alert">
          Your holdings could not be loaded. Try again in a moment.
        </p>
      ) : null}

      {holdings.isSuccess && rows.length === 0 ? (
        <div className="mt-5 rounded-xl border border-hairline bg-card p-[22px]">
          <h2 className="text-[15px] font-semibold text-ink">Nothing here yet</h2>
          <p className="mt-1.5 text-[13px] text-muted">
            Add a holding for each thing you own — a stock, gold, anything with a
            price — then record what you paid and what it is worth today.
          </p>
          <button type="button" className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0" onClick={() => setModalHolding("new")}>
            Add your first holding
          </button>
        </div>
      ) : null}

      {rows.length > 0 ? (
        <ul className="mt-5 flex flex-col gap-3">
          {rows.map((holding) => (
            <HoldingRow
              key={holding.id}
              holding={holding}
              symbolFor={symbolFor}
              busy={pendingIds.has(holding.id)}
              onEdit={() => setModalHolding(holding)}
              onOpenLots={() => setLotsHolding(holding)}
              onArchive={async () => {
                markPending(holding.id, true);
                try {
                  await holdings.archiveHolding.mutateAsync(holding.id);
                } finally {
                  markPending(holding.id, false);
                }
              }}
              onRestore={async () => {
                markPending(holding.id, true);
                try {
                  await holdings.restoreHolding.mutateAsync(holding.id);
                } finally {
                  markPending(holding.id, false);
                }
              }}
            />
          ))}
        </ul>
      ) : null}

      {modalHolding !== null ? (
        <HoldingModal
          holding={modalHolding === "new" ? null : modalHolding}
          onClose={() => setModalHolding(null)}
          create={holdings.createHolding}
          update={holdings.updateHolding}
        />
      ) : null}

      {lotsHolding !== null ? (
        <HoldingLotsPanel holding={lotsHolding} onClose={() => setLotsHolding(null)} />
      ) : null}
    </PageContainer>
  );
}

function HoldingRow(props: {
  holding: Holding;
  symbolFor: (currency: string) => string | undefined;
  busy: boolean;
  onEdit: () => void;
  onOpenLots: () => void;
  onArchive: () => void;
  onRestore: () => void;
}) {
  const { holding, busy, symbolFor } = props;
  const archived = holding.archivedAt !== null;

  return (
    <li className={`flex flex-col gap-4 rounded-xl border border-hairline bg-card p-[22px] ${archived ? "opacity-60" : ""}`}>
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="text-[15px] font-semibold text-ink">
          {holding.name}
          {archived ? <span className="ml-1 text-[11px] font-normal text-muted"> (archived)</span> : null}
        </h2>
        <p className="text-[11.5px] text-muted">
          {INSTRUMENT_LABEL[holding.instrument]} · {holding.accountName}
        </p>
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
        <div>
          <dt className="text-[11px] text-muted">Held</dt>
          {/* The server formatted this string. Nothing here divides heldNano. */}
          <dd className="mt-0.5 text-[14px] text-ink">
            {holding.held} {holding.unit}
          </dd>
        </div>
        <div>
          <dt className="text-[11px] text-muted">Cost</dt>
          <dd className="mt-0.5 text-[14px] text-ink">{formatMoney(holding.costMinor, holding.currency, symbolFor(holding.currency))}</dd>
        </div>
        <div>
          <dt className="text-[11px] text-muted">Worth now</dt>
          <dd className="mt-0.5 text-[14px] text-ink">
            {holding.hasMarketValue ? (
              <>
                {formatMoney(holding.marketValueMinor, holding.currency, symbolFor(holding.currency))}
                {/* The household's own currency beside the instrument's, but
                    only when they differ -- a US stock up in USD while SGD
                    gained against USD made the household poorer, and the
                    primary figure is the one that says so. */}
                {holding.primaryMarketValueMinor !== null && holding.primaryCurrency !== null ? (
                  <span className="text-[12px] text-muted">
                    {" "}
                    ≈ {formatMoney(holding.primaryMarketValueMinor, holding.primaryCurrency, symbolFor(holding.primaryCurrency))}
                  </span>
                ) : null}
                <span className="block text-[11px] text-muted"> as of {holding.valuedAt}</span>
              </>
            ) : (
              // Never a zero. "No price recorded" is a different claim from
              // "worth nothing", and the page has to make the right one.
              <span className="text-[13px] text-muted" data-testid="no-price">
                No price recorded
              </span>
            )}
          </dd>
        </div>
        {holding.realisedMinor !== 0 ? (
          <div>
            <dt className="text-[11px] text-muted">Realised</dt>
            <dd className="mt-0.5 text-[14px] text-ink">{formatMoney(holding.realisedMinor, holding.currency, symbolFor(holding.currency))}</dd>
          </div>
        ) : null}
      </dl>

      <div className="flex flex-wrap gap-2">
        <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={props.onOpenLots}>
          Entries &amp; prices
        </button>
        <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={props.onEdit}>
          Edit
        </button>
        {archived ? (
          <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={props.onRestore} disabled={busy}>
            Restore
          </button>
        ) : (
          <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={props.onArchive} disabled={busy}>
            Archive
          </button>
        )}
      </div>
    </li>
  );
}
