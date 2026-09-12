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
      <header className="page-header">
        <div>
          <h1>Portfolio</h1>
          <p className="page-header__subtitle">
            What you hold, what it cost, and what it is worth.
          </p>
        </div>
        <button type="button" className="button button--primary" onClick={() => setModalHolding("new")}>
          Add holding
        </button>
      </header>

      {holdings.data?.notInNetWorth ? (
        <p className="callout callout--info" data-testid="not-in-net-worth">
          Holdings are not counted in your net worth yet. The figures here stand
          on their own.
        </p>
      ) : null}

      <div className="page-controls">
        <ToggleSwitch
          label="Show archived"
          checked={includeArchived}
          onChange={() => setIncludeArchived((on) => !on)}
        />
      </div>

      {holdings.isPending ? <p>Loading your holdings…</p> : null}
      {holdings.isError ? (
        <p className="form-error" role="alert">
          Your holdings could not be loaded. Try again in a moment.
        </p>
      ) : null}

      {holdings.isSuccess && rows.length === 0 ? (
        <div className="empty-state">
          <h2>Nothing here yet</h2>
          <p>
            Add a holding for each thing you own — a stock, gold, anything with a
            price — then record what you paid and what it is worth today.
          </p>
          <button type="button" className="button button--primary" onClick={() => setModalHolding("new")}>
            Add your first holding
          </button>
        </div>
      ) : null}

      {rows.length > 0 ? (
        <ul className="holding-list">
          {rows.map((holding) => (
            <HoldingRow
              key={holding.id}
              holding={holding}
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
  busy: boolean;
  onEdit: () => void;
  onOpenLots: () => void;
  onArchive: () => void;
  onRestore: () => void;
}) {
  const { holding, busy } = props;
  const archived = holding.archivedAt !== null;

  return (
    <li className={archived ? "holding-row holding-row--archived" : "holding-row"}>
      <div className="holding-row__identity">
        <h2 className="holding-row__name">
          {holding.name}
          {archived ? <span className="badge"> (archived)</span> : null}
        </h2>
        <p className="holding-row__meta">
          {INSTRUMENT_LABEL[holding.instrument]} · {holding.accountName}
        </p>
      </div>

      <dl className="holding-row__figures">
        <div>
          <dt>Held</dt>
          {/* The server formatted this string. Nothing here divides heldNano. */}
          <dd>
            {holding.held} {holding.unit}
          </dd>
        </div>
        <div>
          <dt>Cost</dt>
          <dd>{formatMoney(holding.costMinor, holding.currency)}</dd>
        </div>
        <div>
          <dt>Worth now</dt>
          <dd>
            {holding.hasMarketValue ? (
              <>
                {formatMoney(holding.marketValueMinor, holding.currency)}
                {/* The household's own currency beside the instrument's, but
                    only when they differ -- a US stock up in USD while SGD
                    gained against USD made the household poorer, and the
                    primary figure is the one that says so. */}
                {holding.primaryMarketValueMinor !== null && holding.primaryCurrency !== null ? (
                  <span className="holding-row__primary">
                    {" "}
                    ≈ {formatMoney(holding.primaryMarketValueMinor, holding.primaryCurrency)}
                  </span>
                ) : null}
                <span className="holding-row__as-of"> as of {holding.valuedAt}</span>
              </>
            ) : (
              // Never a zero. "No price recorded" is a different claim from
              // "worth nothing", and the page has to make the right one.
              <span className="holding-row__no-value" data-testid="no-price">
                No price recorded
              </span>
            )}
          </dd>
        </div>
        {holding.realisedMinor !== 0 ? (
          <div>
            <dt>Realised</dt>
            <dd>{formatMoney(holding.realisedMinor, holding.currency)}</dd>
          </div>
        ) : null}
      </dl>

      <div className="holding-row__actions">
        <button type="button" className="button" onClick={props.onOpenLots}>
          Entries &amp; prices
        </button>
        <button type="button" className="button" onClick={props.onEdit}>
          Edit
        </button>
        {archived ? (
          <button type="button" className="button" onClick={props.onRestore} disabled={busy}>
            Restore
          </button>
        ) : (
          <button type="button" className="button" onClick={props.onArchive} disabled={busy}>
            Archive
          </button>
        )}
      </div>
    </li>
  );
}
