// Discard draft, inside the retro modal (design decision 2: "a draft can be
// deleted; a finished retro cannot"). RetroModal renders this only for a
// draft; see the gate there for why. Pulled out of RetroModal.tsx; its
// handler and comments moved here unchanged, and its ask/confirm/cancel state
// is now useConfirmAction's.
//
// TransactionModal.tsx's own delete flow is the shape this mirrors:
// confirmation happens in the page, not `window.confirm`, and its own
// bordered box sits apart from Save draft/Finish retro so no one reaches for
// it by accident. `data-testid="retro-discard-draft"` pins this block's own
// wiring the same way retro-add-action does -- an assertion that finds it, or
// the trigger/confirm controls inside it, would go red if this block's render
// were ever deleted from the modal.
import { useConfirmAction } from "../../components/useConfirmAction";
import { RETRO_COPY } from "./retroCopy";
import type { useRetro } from "./useRetro";

export function DiscardDraftControl({
  disabled,
  discardDraft,
  onDiscarded,
  onClose,
}: {
  // useRetroDraft's `actionsDisabled`: a save in flight, or the conflict latch.
  disabled: boolean;
  discardDraft: ReturnType<typeof useRetro>["discardDraft"];
  // RetroModal's own `onDiscarded` -- see that prop's comment.
  onDiscarded?: () => void;
  onClose: () => void;
}) {
  const discard = useConfirmAction(RETRO_COPY.discardDraftError);

  // Deletes the retro outright -- only ever reachable while it is still a
  // draft (RetroModal's own `completedAt === null` gate), but checked against
  // `disabled` too, the same explicit-invariant reason useRetroDraft's
  // `finish`, CarryOverList and AddActionComposer each repeat it: once
  // `hadConflict` latches, nothing in the modal should still be writing
  // against a retro this tab's own understanding of may already be stale.
  // TransactionModal.tsx's own `handleDelete` is the precedent this mirrors:
  // clear any prior error, mark in flight, await the mutation, close on
  // success, and collapse the confirm/cancel pair back to its resting state
  // regardless of outcome (useConfirmAction does all of that) -- a failed
  // delete should not leave the confirm/cancel pair stuck open forever.
  async function handleDiscardDraft() {
    if (disabled) return;
    await discard.confirm(async () => {
      await discardDraft();
      onDiscarded?.();
      onClose();
    });
  }

  const discardError = discard.errorFor();

  return (
    <div data-testid="retro-discard-draft" className="rounded-[10px] border border-hairline p-3">
      {discard.isConfirming() ? (
        <div className="flex flex-col gap-2.5">
          <p className="text-[12.5px] text-ink">{RETRO_COPY.discardDraftConfirmBody}</p>
          <div className="flex gap-2.5">
            <button
              type="button"
              onClick={discard.cancel}
              className="min-h-11 flex-1 rounded-lg border border-hairline py-2 text-center text-[13px] font-semibold text-label sm:min-h-0"
            >
              {RETRO_COPY.discardDraftCancelAction}
            </button>
            <button
              type="button"
              disabled={discard.isPending() || disabled}
              onClick={() => void handleDiscardDraft()}
              className="min-h-11 flex-1 rounded-lg bg-danger py-2 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              {RETRO_COPY.discardDraftConfirmAction}
            </button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          disabled={disabled}
          onClick={() => discard.ask()}
          className="min-h-11 text-[13px] font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
        >
          {RETRO_COPY.discardDraftTrigger}
        </button>
      )}
      {/* Outside the ternary on purpose, not inside the confirm pair:
          useConfirmAction collapses the pair when the DELETE fails, so a
          message drawn inside it disappeared with it and only showed up if the
          person clicked "Discard draft" again. Here it shows under whichever
          state the control is in. mt-2.5 matches the pair's own gap-2.5. */}
      {discardError && (
        <p role="alert" className="mt-2.5 text-xs leading-snug text-danger">
          {discardError}
        </p>
      )}
    </div>
  );
}
