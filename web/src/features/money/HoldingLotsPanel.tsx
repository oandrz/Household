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
import { Modal } from "../../components/Modal";
import { formatMoney, toMinorUnits } from "./formatMoney";
import { useHoldingEvents, useHoldingValuations, useHoldings } from "./useHoldings";
import type { Holding, HoldingEventKind } from "./holdingSchemas";

export function HoldingLotsPanel({
  holding,
  onClose,
}: {
  holding: Holding;
  onClose: () => void;
}) {
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
  // TransactionModal.tsx's Delete control is the precedent.
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null);

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
      <section className="panel-section">
        <h3>Record a purchase or sale</h3>
        <form onSubmit={submitEvent} className="form form--inline">
          <label className="field">
            <span className="field__label">Kind</span>
            <select
              className="field__input"
              value={kind}
              onChange={(e) => setKind(e.target.value as HoldingEventKind)}
            >
              <option value="acquisition">Bought</option>
              <option value="disposal">Sold</option>
            </select>
          </label>
          <label className="field">
            <span className="field__label">How many {holding.unit}s</span>
            <input
              className="field__input"
              type="text"
              inputMode="decimal"
              value={quantity}
              onChange={(e) => setQuantity(e.target.value)}
              placeholder="300.5"
              required
            />
          </label>
          <label className="field">
            <span className="field__label">
              {kind === "acquisition" ? "Total paid" : "Total received"} ({holding.currency})
            </span>
            <input
              className="field__input"
              type="text"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="1234.56"
              required
            />
          </label>
          <label className="field">
            <span className="field__label">On</span>
            <input
              className="field__input"
              type="date"
              value={occurredOn}
              onChange={(e) => setOccurredOn(e.target.value)}
              required
            />
          </label>
          <button type="submit" className="button button--primary" disabled={recordEvent.isPending}>
            Record
          </button>
        </form>
        {eventError !== null ? (
          <p className="form-error" role="alert">
            {eventError}
          </p>
        ) : null}

        {events.data?.events.length ? (
          <ul className="lot-list">
            {events.data.events.map((event) => (
              <li key={event.id} className="lot-row">
                <span className="lot-row__kind">
                  {event.kind === "acquisition" ? "Bought" : "Sold"}
                </span>
                {/* The server formatted this string; nothing here divides. */}
                <span className="lot-row__quantity">
                  {event.quantity} {holding.unit}
                </span>
                <span className="lot-row__amount">
                  {formatMoney(event.amountMinor, event.currency)}
                </span>
                <span className="lot-row__date">{event.occurredOn}</span>
                {confirmingDelete === event.id ? (
                  <span className="lot-row__confirm">
                    Remove this entry?
                    <button
                      type="button"
                      className="button button--danger"
                      onClick={async () => {
                        try {
                          await deleteEvent.mutateAsync({ id: holding.id, eventId: event.id });
                          setConfirmingDelete(null);
                        } catch (err) {
                          setEventError(
                            err instanceof ApiError ? err.message : "That entry could not be removed.",
                          );
                          setConfirmingDelete(null);
                        }
                      }}
                    >
                      Remove
                    </button>
                    <button type="button" className="button" onClick={() => setConfirmingDelete(null)}>
                      Keep
                    </button>
                  </span>
                ) : (
                  <button type="button" className="button" onClick={() => setConfirmingDelete(event.id)}>
                    Remove
                  </button>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <p className="empty-note">No entries yet.</p>
        )}
      </section>

      <section className="panel-section">
        <h3>Record a price</h3>
        <p className="panel-section__hint">
          What one {holding.unit} was worth on a given day. Re-entering a day's
          price replaces it.
        </p>
        <form onSubmit={submitPrice} className="form form--inline">
          <label className="field">
            <span className="field__label">Price per {holding.unit} ({holding.currency})</span>
            <input
              className="field__input"
              type="text"
              inputMode="decimal"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
              placeholder="130.00"
              required
            />
          </label>
          <label className="field">
            <span className="field__label">As of</span>
            <input
              className="field__input"
              type="date"
              value={asOf}
              onChange={(e) => setAsOf(e.target.value)}
              required
            />
          </label>
          <button
            type="submit"
            className="button button--primary"
            disabled={recordValuation.isPending}
          >
            Save price
          </button>
        </form>
        {priceError !== null ? (
          <p className="form-error" role="alert">
            {priceError}
          </p>
        ) : null}

        {valuations.data?.valuations.length ? (
          <ul className="price-list">
            {valuations.data.valuations.map((valuation) => (
              <li key={valuation.id} className="price-row">
                <span>{formatMoney(valuation.unitPriceMinor, valuation.currency)}</span>
                <span className="price-row__date">as of {valuation.asOf}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="empty-note">No prices recorded yet.</p>
        )}
      </section>
    </Modal>
  );
}

function today(): string {
  return new Date().toISOString().slice(0, 10);
}
