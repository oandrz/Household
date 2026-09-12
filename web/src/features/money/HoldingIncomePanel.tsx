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
import { Modal } from "../../components/Modal";
import { formatMoney, toMinorUnits } from "./formatMoney";
import { useHoldingIncome, useHoldings } from "./useHoldings";
import type { Holding, IncomeKind } from "./holdingSchemas";

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
  // page and, in this project, blocks browser automation outright.
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null);

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
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
              value={kind}
              onChange={(e) => setKind(e.target.value as IncomeKind)}
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
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
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
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
              type="date"
              value={receivedOn}
              onChange={(e) => setReceivedOn(e.target.value)}
              required
            />
          </label>
          <label className="flex flex-1 min-w-[12rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">Note</span>
            <input
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
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
              {confirmingDelete === row.id ? (
                <span className="flex items-center gap-2">
                  <button
                    type="button"
                    className="text-[12px] font-semibold text-danger"
                    onClick={async () => {
                      await deleteIncome.mutateAsync({ id: holding.id, incomeId: row.id });
                      setConfirmingDelete(null);
                    }}
                  >
                    Really remove
                  </button>
                  <button
                    type="button"
                    className="text-[12px] text-muted"
                    onClick={() => setConfirmingDelete(null)}
                  >
                    Keep
                  </button>
                </span>
              ) : (
                <button
                  type="button"
                  className="text-[12px] text-muted"
                  onClick={() => setConfirmingDelete(row.id)}
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
