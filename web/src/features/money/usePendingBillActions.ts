// The Bills page's row actions and their in-flight state: archive and restore
// (tracked per bill id) and undoing a payment (ask, confirm, cancel, with a
// per-payment error). Pulled out of BillsPage.tsx so the page only renders;
// every handler and comment below moved here unchanged. The undo confirmation
// itself is useConfirmAction's, keyed by payment id.
import { useState } from "react";
import { useConfirmAction } from "../../components/useConfirmAction";
import { BILL_COPY } from "./billCopy";
import { useArchiveBill, useRestoreBill, useUndoPayment } from "./useBills";
import type { BillPayment } from "./billSchemas";

export function usePendingBillActions() {
  const archiveBill = useArchiveBill();
  const restoreBill = useRestoreBill();
  const undoPayment = useUndoPayment();

  // Scoped per bill id, not one page-wide flag -- useArchiveBill/useRestoreBill
  // are each one shared mutation instance, so a single mutation's own
  // isPending only ever reflects the most recently dispatched call
  // (AccountsPanel.tsx's own pendingIds carries the identical reasoning).
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());

  // Undo's own in-page confirmation (the same hook GoalContributionsPanel.tsx
  // uses, keyed here by payment id): which row is asking to confirm, which
  // row's DELETE is in flight, and each row's own error, so a 409 on one
  // payment reads next to that payment and not under whichever row is last
  // on screen.
  const undo = useConfirmAction(BILL_COPY.genericSaveError);

  // A stored BILL_PAYMENT_NOT_LATEST message names a due date that WAS a
  // bill's own MAX(due_on) the moment the server answered it. TWO different
  // writes can move that fact out from under the message: a successful
  // UndoPayment (removes the latest, promoting whichever payment was
  // second) and a successful MarkPaid (writes a new payment and advances
  // next_due, which becomes the new latest -- bill.go's own arithmetic).
  // Both call this after their own mutation resolves, rather than each
  // inlining `undo.clearErrors()` separately, so there is exactly one place
  // that states the reason instead of two copies that could drift.
  //
  // Tried first as a derived effect (clearing on `bills`'s own fetch
  // finishing, so no call site could forget it): a `useEffect` keyed on
  // `bills.dataUpdatedAt` never re-fired under test, because that field is
  // stamped from `Date.now()` and every Bills test in this codebase runs
  // under `vi.useFakeTimers({ toFake: ["Date"] })` with the clock frozen
  // (so "today" stays stable for date-prefill assertions) -- every fetch in
  // a test, including a real refetch, stamps the identical millisecond.
  // Switching to `bills.isFetching`'s own true->false transition (clock-
  // independent) still failed: React 18's automatic batching collapsed the
  // mutation's own fetch-refetch cycle into a single commit often enough
  // that the effect's dependency never observed an intermediate `true`, so
  // the effect silently skipped re-running for exactly the writes it
  // existed to catch (confirmed by instrumenting it: only the initial
  // mount's own transition ever fired). An explicit call at each of the
  // two known write sites is less elegant than "cannot be forgotten by a
  // future third site," but it is the one that is actually reliable here,
  // and both existing sites call the same one function so there is still
  // only one place to remember for the two that exist today.
  //
  // Clearing every row's error rather than scoping to the one bill just
  // written is deliberate: a stale error sitting on an unrelated bill's row
  // was never going to be made MORE wrong by clearing it early too, so
  // nothing true is lost -- only ever a possibly-stale refusal, gone one
  // write sooner than strictly necessary.
  function clearUndoErrors() {
    undo.clearErrors();
  }

  function trackPending(id: string, call: Promise<unknown>) {
    setPendingIds((prev) => new Set(prev).add(id));
    void call.finally(() => {
      setPendingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    });
  }

  // The only way a household reaches the archived view at all, and gets
  // back out of it -- this task's own reason for existing (see the header
  // comment above and docs/LEARNING.md pattern 15).
  function handleArchive(id: string) {
    trackPending(id, archiveBill.mutateAsync(id));
  }
  function handleRestore(id: string) {
    trackPending(id, restoreBill.mutateAsync(id));
  }

  // Asking only opens this payment's confirm pair. A stale error from an
  // earlier attempt stays on the row until Undo is confirmed again, and
  // useConfirmAction clears it then (GoalContributionsPanel.tsx's own
  // convention: a fresh attempt starts from a clean slate, not a message from
  // the attempt before it).
  function handleAskUndo(payment: BillPayment) {
    undo.ask(payment.id);
  }
  function handleCancelUndo() {
    undo.cancel();
  }
  // BILL_PAYMENT_NOT_LATEST's own message already names the due date that IS
  // undoable (writeUndoPaymentError, bill_handlers.go) -- apiErrorMessage's
  // verbatim pass-through, inside useConfirmAction, is the whole job here, the
  // same reason MarkPaidModal.tsx's own catch needs no special case. Either
  // way the row collapses back to its plain trigger, and the error (when
  // there is one) is what stays visible on this same row, not the confirm
  // pair.
  async function handleConfirmUndo(payment: BillPayment) {
    await undo.confirm(async () => {
      await undoPayment.mutateAsync({ billId: payment.billId, paymentId: payment.id });
      clearUndoErrors();
    }, payment.id);
  }

  return {
    pendingIds,
    handleArchive,
    handleRestore,
    isConfirmingUndo: undo.isConfirming,
    isUndoing: undo.isPending,
    undoErrorFor: undo.errorFor,
    handleAskUndo,
    handleCancelUndo,
    handleConfirmUndo,
    clearUndoErrors,
  };
}
