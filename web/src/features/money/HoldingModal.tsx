// Add or edit one holding. Mirrors GoalModal.tsx's shape: a Modal, a form that
// owns its own draft state, and the page's mutations passed in rather than a
// second useHoldings() instance issuing its own GET.
//
// The account picker offers INVESTMENT accounts only. The server refuses any
// other type (422 ACCOUNT_NOT_INVESTMENT) and that handler stays, but offering
// a cash account and then failing is the first-run surprise this product was
// asked to stop making. The filter and the error handler are both here for the
// same reason a database CHECK and a Go parser both exist.
import { useState, type FormEvent } from "react";
import { ApiError } from "../../api/client";
import { Modal } from "../../components/Modal";
import { useAccounts } from "./useAccounts";
import type { CreateHoldingBody, UpdateHoldingBody } from "./useHoldings";
import type { Holding, InstrumentKind } from "./holdingSchemas";

type Mutation<TBody> = {
  mutateAsync: (input: TBody) => Promise<unknown>;
  isPending: boolean;
};

const INSTRUMENTS: { value: InstrumentKind; label: string; unit: string }[] = [
  { value: "stock", label: "Stock or fund", unit: "share" },
  { value: "gold", label: "Gold", unit: "gram" },
  { value: "other", label: "Something else", unit: "unit" },
];

export function HoldingModal({
  holding,
  onClose,
  create,
  update,
}: {
  holding: Holding | null;
  onClose: () => void;
  create: Mutation<CreateHoldingBody>;
  update: Mutation<{ id: string; body: UpdateHoldingBody }>;
}) {
  const isEditing = holding !== null;
  const accounts = useAccounts(false);
  const [name, setName] = useState(holding?.name ?? "");
  const [instrument, setInstrument] = useState<InstrumentKind>(holding?.instrument ?? "stock");
  const [unit, setUnit] = useState(holding?.unit ?? "share");
  const [accountId, setAccountId] = useState(holding?.accountId ?? "");
  const [error, setError] = useState<string | null>(null);

  const investmentAccounts = (accounts.data?.accounts ?? []).filter(
    (account) => account.type === "investment",
  );

  const onInstrumentChange = (next: InstrumentKind) => {
    setInstrument(next);
    // The unit follows the instrument unless the person has already said
    // otherwise; gold is weighed in grams and a stock is counted in shares, and
    // making them type that every time is friction for no gain.
    const preset = INSTRUMENTS.find((i) => i.value === next);
    if (preset && !isEditing) setUnit(preset.unit);
  };

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    try {
      if (isEditing) {
        await update.mutateAsync({ id: holding.id, body: { name, instrument, unit } });
      } else {
        await create.mutateAsync({ accountId, name, instrument, unit });
      }
      onClose();
    } catch (err) {
      // The server's own message is shown rather than a generic one: it is the
      // sentence that knows which rule refused, including the archived-name
      // conflict that offers Restore.
      setError(err instanceof ApiError ? err.message : "That could not be saved. Try again.");
    }
  };

  const busy = create.isPending || update.isPending;

  return (
    <Modal open onClose={onClose} title={isEditing ? "Edit holding" : "Add a holding"}>
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        {!isEditing ? (
          <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
            <span className="text-xs font-semibold text-label">Account</span>
            <select
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
              value={accountId}
              onChange={(e) => setAccountId(e.target.value)}
              required
            >
              <option value="">Choose an investment account…</option>
              {investmentAccounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.nickname}
                </option>
              ))}
            </select>
            {accounts.isSuccess && investmentAccounts.length === 0 ? (
              <span className="text-[11.5px] leading-snug text-muted">
                You have no investment accounts yet. Add one on Finances first —
                holdings live inside one.
              </span>
            ) : null}
          </label>
        ) : null}

        <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
          <span className="text-xs font-semibold text-label">Name</span>
          <input
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="D05, Gold bar, …"
            required
          />
        </label>

        <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
          <span className="text-xs font-semibold text-label">Kind</span>
          <select
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            value={instrument}
            onChange={(e) => onInstrumentChange(e.target.value as InstrumentKind)}
          >
            {INSTRUMENTS.map((i) => (
              <option key={i.value} value={i.value}>
                {i.label}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-1 min-w-[9rem] flex-col gap-1.5">
          <span className="text-xs font-semibold text-label">Counted in</span>
          <input
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            value={unit}
            onChange={(e) => setUnit(e.target.value)}
            placeholder="share, gram, unit"
            required
          />
          <span className="text-[11.5px] leading-snug text-muted">What one of it is called — shares, grams, units.</span>
        </label>

        {error !== null ? (
          <p className="text-xs leading-snug text-danger" role="alert">
            {error}
          </p>
        ) : null}

        <div className="flex justify-end gap-2">
          <button type="button" className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] font-semibold text-ink sm:min-h-0" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0" disabled={busy}>
            {isEditing ? "Save" : "Add holding"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
