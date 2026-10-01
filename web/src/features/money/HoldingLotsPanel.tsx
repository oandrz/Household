// One holding's entries and prices: the purchases and sales it is made of, and
// the prices someone has recorded for it. Mirrors GoalContributionsPanel.tsx --
// a Modal over the child ledger, with its own small forms.
//
// The quantity input is TEXT, and what is typed is sent verbatim. It is never
// converted to nano units here: that conversion is a multiply by 1e9 in
// float64, on a figure a money screen shows, which is the defect
// docs/LEARNING.md already records this codebase shipping once. The server
// parses it in integers and sends back a formatted string to render.
import { useState, type FormEvent } from "react";
import { ApiError } from "../../api/client";
import { useCurrencies } from "../auth/useAuth";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { Modal } from "../../components/Modal";
import { useConfirmAction } from "../../components/useConfirmAction";
import { formatMoney, toMinorUnits } from "./formatMoney";
import { useHoldingEvents, useHoldingValuations, useHoldings } from "./useHoldings";
import { parseEnum } from "../../lib/parseEnum";
import { holdingEventKindSchema, type Holding, type HoldingEventKind } from "./holdingSchemas";

export function HoldingLotsPanel({
  holding,
  onClose,
}: {
  holding: Holding;
  onClose: () => void;
}) {
  const currencies = useCurrencies();
  // BillsPage.tsx's symbolFor, for its reason: without the symbol formatMoney
  // falls back to the bare currency code, which no other money screen shows.
  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;
  const events = useHoldingEvents(holding.id);
  const valuations = useHoldingValuations(holding.id);
  // enabled: false -- this panel never reads the portfolio list itself, it only
  // writes. A write still invalidates the page's own mounted instance, which is
  // the one that has to refetch. GoalModal.tsx's precedent.
  const { recordEvent, deleteEvent, recordValuation } = useHoldings({
    includeArchived: false,
    enabled: false,
  });

  const [kind, setKind] = useState<HoldingEventKind>("acquisition");
  const [quantity, setQuantity] = useState("");
  const [amount, setAmount] = useState("");
  const [occurredOn, setOccurredOn] = useState(today());
  const [eventError, setEventError] = useState<string | null>(null);

  const [price, setPrice] = useState("");
  const [asOf, setAsOf] = useState(today());
  const [priceError, setPriceError] = useState<string | null>(null);

  // An in-page confirmation, never window.confirm: a native dialog blocks the
  // page and, in this project, blocks browser automation outright.
  // TransactionModal.tsx's Delete control is the precedent. Keyed by entry id.
  const entryRemoval = useConfirmAction();

  const submitEvent = async (e: FormEvent) => {
    e.preventDefault();
    setEventError(null);
    const amountMinor = toMinorUnits(amount, holding.currency);
    if (amountMinor === null) {
      setEventError(`Enter an amount, like 1234.56.`);
      return;
    }
    try {
      await recordEvent.mutateAsync({
        id: holding.id,
        // quantity goes as typed. No conversion here, deliberately.
        body: { kind, quantity, amountMinor, occurredOn, note: "" },
      });
      setQuantity("");
      setAmount("");
    } catch (err) {
      setEventError(err instanceof ApiError ? err.message : "That entry could not be saved.");
    }
  };

  const submitPrice = async (e: FormEvent) => {
    e.preventDefault();
    setPriceError(null);
    const unitPriceMinor = toMinorUnits(price, holding.currency);
    if (unitPriceMinor === null) {
      setPriceError("Enter a price per unit, like 1234.56.");
      return;
    }
    try {
      await recordValuation.mutateAsync({
        id: holding.id,
        body: { unitPriceMinor, asOf, note: "" },
      });
      setPrice("");
    } catch (err) {
      setPriceError(err instanceof ApiError ? err.message : "That price could not be saved.");
    }
  };

  return (
    <Modal open onClose={onClose} title={`${holding.name} — entries & prices`} wide>
      <section className="mt-2 flex flex-col gap-3 border-t border-hairline pt-4 first:mt-0 first:border-0 first:pt-0">
        <h3 className="text-[13px] font-semibold text-ink">Record a purchase or sale</h3>
        <form onSubmit={submitEvent} className="flex flex-wrap items-end gap-3">
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">Kind</span>
            <select
              className={FIELD_CONTROL_CLASS}
              value={kind}
              onChange={(e) => setKind(parseEnum(e.target.value, holdingEventKindSchema.options, kind))}
            >
              <option value="acquisition">Bought</option>
              <option value="disposal">Sold</option>
            </select>
          </label>
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">How many {holding.unit}s</span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="text"
              inputMode="decimal"
              value={quantity}
              onChange={(e) => setQuantity(e.target.value)}
              placeholder="300.5"
              required
            />
          </label>
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">
              {kind === "acquisition" ? "Total paid" : "Total received"} ({holding.currency})
            </span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="text"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="1234.56"
              required
            />
          </label>
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">On</span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="date"
              value={occurredOn}
              onChange={(e) => setOccurredOn(e.target.value)}
              required
            />
          </label>
          <button type="submit" className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0" disabled={recordEvent.isPending}>
            Record
          </button>
        </form>
        {eventError !== null ? (
          <p className="text-xs leading-snug text-danger" role="alert">
            {eventError}
          </p>
        ) : null}

        {events.data?.events.length ? (
          <ul className="flex flex-col gap-1.5">
            {events.data.events.map((event) => (
              <li key={event.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg bg-surface px-3 py-2 text-[12.5px]">
                <span className="font-semibold text-ink">
                  {event.kind === "acquisition" ? "Bought" : "Sold"}
                </span>
                {/* The server formatted this string; nothing here divides. */}
                <span className="text-ink">
                  {event.quantity} {holding.unit}
                </span>
                <span className="text-ink">
                  {formatMoney(event.amountMinor, event.currency, symbolFor(event.currency))}
                </span>
                <span className="text-muted">{event.occurredOn}</span>
                {entryRemoval.isConfirming(event.id) ? (
                  <span className="flex flex-wrap items-center gap-2 text-muted">
                    Remove this entry?
                    <button
                      type="button"
                      // Off while this entry's DELETE is in flight, so a double
                      // click cannot send a second request for an entry the
                      // first one is already removing. It reads the hook's
                      // per-entry flag, not deleteEvent.isPending: one mutation
                      // serves every row, so its flag would grey every row.
                      disabled={entryRemoval.isPending(event.id)}
                      className="min-h-11 rounded-lg bg-danger px-3.5 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
                      onClick={() =>
                        void entryRemoval.confirm(async () => {
                          // A failure shows in this section's own error line,
                          // shared with the form above, so it is caught here
                          // rather than kept against the row. The hook closes
                          // the confirmation either way.
                          try {
                            await deleteEvent.mutateAsync({ id: holding.id, eventId: event.id });
                          } catch (err) {
                            setEventError(
                              err instanceof ApiError ? err.message : "That entry could not be removed.",
                            );
                          }
                        }, event.id)
                      }
                    >
                      Remove
                    </button>
                    <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={entryRemoval.cancel}>
                      Keep
                    </button>
                  </span>
                ) : (
                  <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={() => entryRemoval.ask(event.id)}>
                    Remove
                  </button>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-xs text-muted">No entries yet.</p>
        )}
      </section>

      <section className="mt-2 flex flex-col gap-3 border-t border-hairline pt-4 first:mt-0 first:border-0 first:pt-0">
        <h3 className="text-[13px] font-semibold text-ink">Record a price</h3>
        <p className="text-[11.5px] leading-snug text-muted">
          What one {holding.unit} was worth on a given day. Re-entering a day's
          price replaces it.
        </p>
        <form onSubmit={submitPrice} className="flex flex-wrap items-end gap-3">
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">Price per {holding.unit} ({holding.currency})</span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="text"
              inputMode="decimal"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
              placeholder="130.00"
              required
            />
          </label>
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">As of</span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="date"
              value={asOf}
              onChange={(e) => setAsOf(e.target.value)}
              required
            />
          </label>
          <button
            type="submit"
            className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0"
            disabled={recordValuation.isPending}
          >
            Save price
          </button>
        </form>
        {priceError !== null ? (
          <p className="text-xs leading-snug text-danger" role="alert">
            {priceError}
          </p>
        ) : null}

        {valuations.data?.valuations.length ? (
          <ul className="flex flex-col gap-1.5">
            {valuations.data.valuations.map((valuation) => (
              <li key={valuation.id} className="flex flex-wrap items-center gap-x-3 rounded-lg bg-surface px-3 py-2 text-[12.5px] text-ink">
                <span>{formatMoney(valuation.unitPriceMinor, valuation.currency, symbolFor(valuation.currency))}</span>
                <span className="text-muted">as of {valuation.asOf}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-xs text-muted">No prices recorded yet.</p>
        )}
      </section>
    </Modal>
  );
}

// The LOCAL calendar day, never toISOString().slice(0, 10) -- that renders in
// UTC, so east of Greenwich it returns yesterday for the first hours of every
// day. In Singapore (UTC+8) that is midnight to 08:00, and a price stamped a
// day early can be silently outranked by an older one, because
// ListLatestValuations orders by as_of.
//
// This repo has shipped this exact mistake three times before (f61407d,
// f17be2d, and the plan correction behind them); AccountModal.tsx and
// GoalContributionsPanel.tsx each carry this same helper for the same reason.
// Each keeps its own copy deliberately rather than coupling three features'
// date handling through one import.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}
