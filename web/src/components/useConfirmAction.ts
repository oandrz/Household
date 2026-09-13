// Ask, then confirm or cancel, for an action that cannot be taken back --
// Delete, Discard, Remove, Undo payment. The confirmation happens in the page,
// never through window.confirm: a native dialog blocks every event in the tab,
// and in this project it blocks the automated browser walks outright
// (TransactionModal.tsx's onDelete comment).
//
// Extracted because six places had hand-written the same three pieces of
// state -- which item is asking to be confirmed, which item's request is in
// flight, and the error that request left behind: RetroModal's Discard draft,
// TransactionModal's Delete, the row removals in GoalContributionsPanel,
// HoldingLotsPanel and HoldingIncomePanel, and usePendingBillActions' Undo
// payment.
//
// `key` says WHICH item is being confirmed, so one hook serves a list where
// every row has its own confirm pair. A screen with only one thing to confirm
// leaves the key out everywhere, and the hook uses one fixed key for it.
//
// The contract every caller relies on:
// - `confirm` clears that item's previous error, runs the action, and records
//   a failure as a message against that item only.
// - Whatever the outcome, the confirm pair collapses back to its trigger
//   afterwards. A failed request must not leave Cancel/Confirm stuck open;
//   the error message is what stays on screen.
// - `ask` does NOT clear an earlier error. Every caller that reads `errorFor`
//   draws the message beside the item, outside its confirm pair (it has to:
//   the pair collapses on failure, so a message inside it is never seen). The
//   message is therefore still on screen when the person asks again, which is
//   exactly when they need it, while deciding whether to retry. Only
//   `confirm` clears it, as the new attempt starts.
//
// A caller that already owns an error line shared with its form
// (TransactionModal, both Holding panels) catches the failure inside the
// action it passes, sets its own line, and never reads `errorFor`.
import { useState } from "react";
import { apiErrorMessage } from "../api/errorMessage";

const SINGLE_ITEM = "single-item";
const GENERIC_FAILURE = "Something went wrong. Please try again.";

export function useConfirmAction(fallbackMessage: string = GENERIC_FAILURE) {
  const [confirmingKey, setConfirmingKey] = useState<string | null>(null);
  const [pendingKey, setPendingKey] = useState<string | null>(null);
  // Keyed like the rest, so a 404 on row 1 of four reads next to row 1 rather
  // than under whichever row is last, and a later success on row 3 leaves row
  // 1's message where it is.
  const [errors, setErrors] = useState<Record<string, string>>({});

  function ask(key: string = SINGLE_ITEM) {
    setConfirmingKey(key);
  }

  function cancel() {
    setConfirmingKey(null);
  }

  async function confirm(action: () => Promise<unknown>, key: string = SINGLE_ITEM): Promise<void> {
    setErrors((prev) => withoutKey(prev, key));
    setPendingKey(key);
    try {
      await action();
    } catch (err) {
      const message = apiErrorMessage(err, fallbackMessage);
      setErrors((prev) => ({ ...prev, [key]: message }));
    } finally {
      setPendingKey(null);
      setConfirmingKey(null);
    }
  }

  return {
    isConfirming: (key: string = SINGLE_ITEM) => confirmingKey === key,
    isPending: (key: string = SINGLE_ITEM) => pendingKey === key,
    errorFor: (key: string = SINGLE_ITEM): string | null => errors[key] ?? null,
    ask,
    cancel,
    confirm,
    clearErrors: () => setErrors({}),
  };
}

function withoutKey(errors: Record<string, string>, key: string): Record<string, string> {
  const next = { ...errors };
  delete next[key];
  return next;
}
