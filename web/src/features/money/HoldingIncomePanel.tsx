// One holding's dividends and charges. Mirrors HoldingLotsPanel.tsx -- a Modal
// over a child ledger with its own small form.
//
// Income sits BESIDE the position rather than inside it: a dividend changes
// neither what is held nor what it cost, so recording one here moves no
// quantity and no cost basis. That is also why a write from this panel
// invalidates the period report and this list, and leaves the portfolio's own
// figures alone (useHoldings.ts's invalidateAfterIncomeWrite).
//
// A fee is entered POSITIVE, like a dividend. The server subtracts fees when it
// sums a period; a negative amount is refused everywhere in this product, and
// asking someone to type a minus sign is how a fee eventually gets entered as
// income by mistake.
import { useState, type FormEvent } from "react";
import { ApiError } from "../../api/client";
import { useCurrencies } from "../auth/useAuth";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { Modal } from "../../components/Modal";
import { useConfirmAction } from "../../components/useConfirmAction";
import { formatMoney, toMinorUnits } from "./formatMoney";
import { useHoldingIncome, useHoldings } from "./useHoldings";
import { parseEnum } from "../../lib/parseEnum";
import { incomeKindSchema, type Holding, type IncomeKind } from "./holdingSchemas";

// Local-time date, never toISOString(): that renders in UTC, so between
// midnight and 8am in this household's own timezone every entry would default
// to yesterday. HoldingLotsPanel.tsx and AccountModal.tsx each carry their own
// copy, deliberately not coupled through one import.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function HoldingIncomePanel({
  holding,
  onClose,
}: {
  holding: Holding;
  onClose: () => void;
}) {
  const currencies = useCurrencies();
  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;
  const income = useHoldingIncome(holding.id);
  // enabled: false -- this panel writes but never reads the portfolio list;
  // the page's own mounted instance is the one that refetches.
  const { recordIncome, deleteIncome } = useHoldings({ includeArchived: false, enabled: false });

  const [kind, setKind] = useState<IncomeKind>("income");
  const [amount, setAmount] = useState("");
  const [receivedOn, setReceivedOn] = useState(today());
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  // An in-page confirmation, never window.confirm: a native dialog blocks the
  // page and, in this project, blocks browser automation outright. Keyed by
  // income row id.
  const rowRemoval = useConfirmAction();

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    const amountMinor = toMinorUnits(amount, holding.currency);
    if (amountMinor === null) {
      setError("Enter an amount, like 45.00.");
      return;
    }
    try {
      await recordIncome.mutateAsync({
        id: holding.id,
        body: { kind, amountMinor, receivedOn, note },
      });
      setAmount("");
      setNote("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "That entry could not be saved.");
    }
  };

  const rows = income.data?.income ?? [];

  return (
    <Modal open onClose={onClose} title={`${holding.name} — dividends & fees`} wide>
      <section className="mt-2 flex flex-col gap-3">
        <h3 className="text-[13px] font-semibold text-ink">Record a payment or a charge</h3>
        <form onSubmit={submit} className="flex flex-wrap items-end gap-3">
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">Kind</span>
            <select
              className={FIELD_CONTROL_CLASS}
              value={kind}
              onChange={(e) => setKind(parseEnum(e.target.value, incomeKindSchema.options, kind))}
            >
              <option value="income">Paid to you</option>
              <option value="fee">Charged to you</option>
            </select>
          </label>
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">
              How much ({holding.currency})
            </span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="text"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="45.00"
              required
            />
          </label>
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">On</span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="date"
              value={receivedOn}
              onChange={(e) => setReceivedOn(e.target.value)}
              required
            />
          </label>
          <label className="flex flex-1 min-w-[12rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">Note</span>
            <input
              className={FIELD_CONTROL_CLASS}
              type="text"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Q3 dividend"
            />
          </label>
          <button
            type="submit"
            className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0"
            disabled={recordIncome.isPending}
          >
            {recordIncome.isPending ? "Saving…" : "Record"}
          </button>
        </form>
        <p className="text-[11.5px] text-muted">
          A charge is entered as a positive amount and comes off the total.
        </p>
        {error ? (
          <p className="text-xs text-danger" role="alert">
            {error}
          </p>
        ) : null}
      </section>

      <section className="mt-4 flex flex-col gap-2 border-t border-hairline pt-4">
        <h3 className="text-[13px] font-semibold text-ink">Recorded so far</h3>
        {income.isPending ? <p className="text-xs text-muted">Loading…</p> : null}
        {income.isSuccess && rows.length === 0 ? (
          <p data-testid="no-income" className="text-[12.5px] text-muted">
            Nothing recorded yet. Dividends and charges go here; buying and
            selling goes under entries.
          </p>
        ) : null}
        <ul className="flex flex-col gap-2">
          {rows.map((row) => (
            <li
              key={row.id}
              data-testid="income-row"
              className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-hairline px-3 py-2 text-[12.5px]"
            >
              <span className="text-ink">
                {row.kind === "fee" ? "Charged" : "Paid"} {formatMoney(row.amountMinor, row.currency, symbolFor(row.currency))}
                <span className="ml-2 text-muted">{row.receivedOn}</span>
                {row.note ? <span className="ml-2 text-muted">{row.note}</span> : null}
              </span>
              {rowRemoval.isConfirming(row.id) ? (
                <span className="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    // Off while this row's DELETE is in flight, so a double
                    // click cannot send a second request for a row the first
                    // one is already removing. It reads the hook's per-row
                    // flag, not deleteIncome.isPending: one mutation serves
                    // every row, so its flag would grey every row at once.
                    disabled={rowRemoval.isPending(row.id)}
                    className="min-h-11 rounded-lg bg-danger px-3.5 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
                    onClick={() =>
                      // useConfirmAction closes the confirmation either way:
                      // a confirmation left open after the answer arrived is
                      // a second trap.
                      void rowRemoval.confirm(async () => {
                        // Caught, like HoldingLotsPanel.tsx's own delete and
                        // unlike the first version of this one: an awaited
                        // mutation with no catch leaves the row on screen, the
                        // confirmation stuck open, nothing said, and an unhandled
                        // rejection in the console. The person concludes the
                        // button is broken. The message goes to this panel's
                        // one error line, shared with the form above.
                        try {
                          await deleteIncome.mutateAsync({ id: holding.id, incomeId: row.id });
                        } catch (err) {
                          setError(
                            err instanceof ApiError ? err.message : "That entry could not be removed.",
                          );
                        }
                      }, row.id)
                    }
                  >
                    Really remove
                  </button>
                  <button
                    type="button"
                    className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0"
                    onClick={rowRemoval.cancel}
                  >
                    Keep
                  </button>
                </span>
              ) : (
                <button
                  type="button"
                  className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0"
                  onClick={() => rowRemoval.ask(row.id)}
                >
                  Remove
                </button>
              )}
            </li>
          ))}
        </ul>
      </section>
    </Modal>
  );
}
