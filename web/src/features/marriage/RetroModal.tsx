// The Start/Edit retro modal (design/Household Dashboard.dc.html's
// `modalRetro` panel): the mood picker, the two textareas, the money
// check-in and actions block (both Task 14's own job -- this task only
// reserves their mount points in the design's order), and Save draft /
// Finish retro. Built on `components/Modal` the same way every other
// feature modal is (GoalModal.tsx's own shape is the closest precedent:
// this component calls `useRetro(month)` itself rather than taking a
// mutation as a prop, because nothing upstream has already resolved one).
//
// `notes` is a real field here even though dc.html's modal never draws a
// third textarea for it -- `saveRetroRequest`/`retroDTO` both carry `notes`
// as a field distinct from wentWell/wasHard (retro_handlers.go), the
// history row's own quoted line is derived from it and not from either
// bullet field (design spec decision 7), and RetroDetail.tsx already
// renders it in its own "Notes" card. The modal is the only place any of
// the retro's four writable fields (mood, wentWell, wasHard, notes) is ever
// typed, so leaving this one out would make it permanently unreachable
// through the UI. Placed after wentWell/wasHard, before the money check-in
// mount point -- the same order SaveRetroBody's own fields are declared in
// useRetro.ts, since the mockup gives no order of its own to follow.
import { type FormEvent, useEffect, useId, useState } from "react";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { Modal } from "../../components/Modal";
import { FieldPair } from "../../components/FieldPair";
import { RETRO_COPY, monthYearLabel } from "./retroCopy";
import { useRetro, type SaveRetroBody } from "./useRetro";

export function RetroModal({ month, onClose }: { month: string; onClose: () => void }) {
  const retro = useRetro(month);

  // Local, editable copies of the four writable fields. Seeded from the
  // server's own loaded record exactly once (the `initialized` guard below)
  // -- never re-seeded on a later successful refetch, which is also what
  // keeps the conflict banner honest: a 409 never touches this state at
  // all, so whatever the household typed stays on screen through it
  // (SaveRetroBody's own comment: the modal, unlike a bare PATCH caller,
  // holds all four fields itself and must pass all four on every save
  // rather than leaning on useRetro's merge-from-server-state fallback).
  const [initialized, setInitialized] = useState(false);
  const [mood, setMood] = useState<number | null>(null);
  const [wentWell, setWentWell] = useState("");
  const [wasHard, setWasHard] = useState("");
  const [notes, setNotes] = useState("");

  const [saveError, setSaveError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [isFinishing, setIsFinishing] = useState(false);
  // A one-way latch, deliberately NOT the same thing as `retro.conflict`
  // (useRetro.ts's own derived, two-way flag). `retro.conflict` is designed
  // to clear itself on ANY later successful refetch of this query --
  // including one this modal never asked for, like React Query's default
  // refetch-on-window-focus -- which is exactly right for a page that only
  // ever *displays* server state. This modal also holds four fields of
  // local, unsaved state that a clearing `conflict` says nothing about: a
  // browser walk against a real 409 proved that once `conflict` clears, the
  // very next Save re-sends whatever is still sitting in these fields
  // together with the now-current version, silently overwriting whatever
  // the partner just saved -- last write wins, with no error and no trace,
  // the exact shape the design spec's decision 6 rejects by name. Once
  // true, this never goes false again for the life of this mount; the only
  // way back to writable is closing the modal and reopening it, which is a
  // fresh mount that seeds these fields from a fresh fetch.
  const [hadConflict, setHadConflict] = useState(false);

  useEffect(() => {
    if (retro.data && !initialized) {
      setMood(retro.data.retro.mood);
      setWentWell(retro.data.retro.wentWell);
      setWasHard(retro.data.retro.wasHard);
      setNotes(retro.data.retro.notes);
      setInitialized(true);
    }
  }, [retro.data, initialized]);

  const moodLegendId = useId();
  const wentWellId = useId();
  const wasHardId = useId();
  const notesId = useId();

  function currentBody(): SaveRetroBody {
    return { mood, wentWell, wasHard, notes };
  }

  // Shared by both handlers below: RETRO_CHANGED latches `hadConflict`
  // (see its own comment) instead of being treated as a generic failure --
  // useRetro's own onError already recorded the narrower `conflict`, and
  // the banner renders from `hadConflict`, not from `saveError`. Returns
  // whether it handled the error, so callers know not to also set
  // `saveError` for the same failure.
  function noteIfConflict(err: unknown): boolean {
    if (err instanceof ApiError && err.code === "RETRO_CHANGED") {
      setHadConflict(true);
      return true;
    }
    return false;
  }

  async function handleSaveDraft() {
    setSaveError(null);
    setIsSaving(true);
    try {
      await retro.saveRetro(currentBody());
      onClose();
    } catch (err) {
      if (!noteIfConflict(err)) {
        setSaveError(apiErrorMessage(err, RETRO_COPY.modalSaveError));
      }
    } finally {
      setIsSaving(false);
    }
  }

  // Finish saves first, then completes -- in that order. Swapping them would
  // complete the retro against whatever the server already had on file and
  // only then send the currently-typed text, discarding it: a finish that
  // "completes then saves" throws away the very thing the household just
  // spent ten minutes writing.
  async function handleFinish(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaveError(null);
    setIsFinishing(true);
    try {
      await retro.saveRetro(currentBody());
      await retro.finishRetro();
      onClose();
    } catch (err) {
      if (!noteIfConflict(err)) {
        setSaveError(apiErrorMessage(err, RETRO_COPY.modalSaveError));
      }
    } finally {
      setIsFinishing(false);
    }
  }

  // Disables both actions below for two different reasons: a write already
  // in flight (transient), or `hadConflict` (permanent for this mount --
  // enabling them again on the strength of `retro.conflict` alone, which
  // clears on any successful refetch including one this modal never asked
  // for, is the exact gap the latch exists to close; see its own comment).
  const actionsDisabled = isSaving || isFinishing || hadConflict;

  return (
    <Modal open onClose={onClose} title={`${monthYearLabel(month)} retro`}>
      <p className="-mt-2 mb-4 text-xs text-muted">{RETRO_COPY.modalPrivacyBadge}</p>

      {retro.loading ? (
        <p className="text-xs text-muted">Loading…</p>
      ) : retro.error ? (
        <p role="alert" data-testid="retro-modal-load-error" className="text-xs text-danger">
          {RETRO_COPY.detailLoadError}
        </p>
      ) : !retro.data ? null : (
        <form className="flex flex-col gap-4" onSubmit={handleFinish}>
          <div>
            <h3 id={moodLegendId} className="mb-2.5 text-xs font-semibold text-label">
              {RETRO_COPY.moodQuestion}
            </h3>
            {/* A real radio group -- five `<input type="radio">`, one tab
                stop, arrow keys between options, all for free once every
                option shares `name="retro-mood"` -- NOT `sr-only` inputs
                behind a styled stand-in. That exact shape shipped
                keyboard-invisible focus in TransactionsPage's Kind filter
                (docs/LEARNING.md pattern 3): the visible pill never reacted
                to the hidden input's own focus, so Tab and the arrow keys
                moved real focus with nothing visible on screen, and no unit
                test caught it because `fireEvent.click` fires straight at
                the element rather than pressing a key the way a real
                keyboard user does. Keeping the input itself visible (16px,
                not hidden) means there is no gap between "what has focus"
                and "what is drawn with a ring" for any CSS here to get
                wrong -- the browser's own focus ring lands on a real,
                on-screen element, the same fix RetroActionRow.tsx's
                checkbox already relies on. */}
            <div role="radiogroup" aria-labelledby={moodLegendId} className="flex gap-1">
              {RETRO_COPY.moodOptions.map((option) => {
                const inputId = `retro-mood-${month}-${option.value}`;
                const selected = mood === option.value;
                return (
                  <label
                    key={option.value}
                    htmlFor={inputId}
                    // min-h-11 is the 44px touch floor -- see this task's
                    // own report for the real, browser-measured width of
                    // each of these five tiles at 320px: this is the
                    // tightest row in the whole feature and the floor may
                    // not clear on both axes there.
                    className={`flex min-h-11 flex-1 cursor-pointer flex-col items-center justify-center gap-1 rounded-[10px] border py-2 text-[18px] sm:min-h-0 ${
                      selected ? "border-accent bg-callout" : "border-hairline"
                    }`}
                  >
                    <span aria-hidden="true">{option.emoji}</span>
                    <input
                      id={inputId}
                      type="radio"
                      name="retro-mood"
                      checked={selected}
                      onChange={() => setMood(option.value)}
                      aria-label={option.label}
                      className="h-4 w-4 accent-accent"
                    />
                  </label>
                );
              })}
            </div>
          </div>

          <FieldPair>
            <div className="flex flex-col gap-1.5">
              <label htmlFor={wentWellId} className="text-xs font-semibold text-accent">
                {RETRO_COPY.wentWellHeading}
              </label>
              <textarea
                id={wentWellId}
                value={wentWell}
                onChange={(event) => setWentWell(event.target.value)}
                placeholder={RETRO_COPY.wentWellPlaceholder}
                rows={4}
                className="min-h-24 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <label htmlFor={wasHardId} className="text-xs font-semibold text-danger">
                {RETRO_COPY.wasHardHeading}
              </label>
              <textarea
                id={wasHardId}
                value={wasHard}
                onChange={(event) => setWasHard(event.target.value)}
                placeholder={RETRO_COPY.wasHardPlaceholder}
                rows={4}
                className="min-h-24 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
              />
            </div>
          </FieldPair>

          <div className="flex flex-col gap-1.5">
            <label htmlFor={notesId} className="text-xs font-semibold text-label">
              {RETRO_COPY.notesHeading}
            </label>
            <textarea
              id={notesId}
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              placeholder={RETRO_COPY.notesPlaceholder}
              rows={3}
              className="min-h-20 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
            />
          </div>

          {/* Task 14 mounts <MoneyCheckInPanel month={month} /> here, and the
              "Still open from {month}" carry-over offer plus "+ Add an
              action" control below it -- this task only reserves the two
              mount points, in the design's own order (title, mood,
              wentWell/wasHard, notes, money check-in, actions,
              Save/Finish). */}
          <div data-testid="retro-modal-money-mount" />
          <div data-testid="retro-modal-actions-mount" />

          {/* Renders from `hadConflict`, the local one-way latch, NOT
              `retro.conflict` -- see hadConflict's own comment on why a
              two-way flag that can clear itself out from under this modal
              (a background refetch-on-window-focus, for instance) must
              never be what decides whether this banner -- or the buttons
              below it -- are showing. No Reload control here on purpose;
              conflictBanner's own comment has the browser-walk-verified
              reason one was tried and removed. */}
          {hadConflict && (
            <div
              data-testid="retro-conflict"
              role="alert"
              className="flex flex-col gap-2 rounded-[10px] border border-hairline bg-callout p-3.5 text-[12.5px] leading-relaxed text-ink"
            >
              <p>{RETRO_COPY.conflictBanner}</p>
            </div>
          )}

          {saveError && (
            <p role="alert" className="text-xs leading-snug text-danger">
              {saveError}
            </p>
          )}

          <div className="mt-1 flex gap-2.5">
            <button
              type="button"
              disabled={actionsDisabled}
              onClick={() => void handleSaveDraft()}
              className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              {RETRO_COPY.saveDraft}
            </button>
            <button
              type="submit"
              disabled={actionsDisabled}
              className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              {RETRO_COPY.finishRetro}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}
