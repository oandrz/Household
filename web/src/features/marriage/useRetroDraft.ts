// The retro modal's own draft of the four writable fields -- mood, wentWell,
// wasHard, notes -- and the two writes that send it: Save draft and Finish
// retro. Pulled out of RetroModal.tsx so the modal only renders; the state,
// handlers and comments below moved here unchanged.
//
// Takes the modal's own `useRetro(month)` result rather than calling that
// hook a second time: CarryOverList, AddActionComposer and DiscardDraftControl
// all write through the same instance, and a second call would be a second,
// separate copy of its `conflict` state.
import { useEffect, useState } from "react";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "../../api/errorMessage";
import { RETRO_COPY } from "./retroCopy";
import type { SaveRetroBody, useRetro } from "./useRetro";

export function useRetroDraft(retro: ReturnType<typeof useRetro>, onClose: () => void) {
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

  // Disables every write the modal offers for two different reasons: a write
  // already in flight (transient), or `hadConflict` (permanent for this mount
  // -- enabling them again on the strength of `retro.conflict` alone, which
  // clears on any successful refetch including one this modal never asked
  // for, is the exact gap the latch exists to close; see its own comment).
  const actionsDisabled = isSaving || isFinishing || hadConflict;

  function currentBody(): SaveRetroBody {
    return { mood, wentWell, wasHard, notes };
  }

  // Shared by both writes below: RETRO_CHANGED latches `hadConflict`
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

  async function saveDraft() {
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
  // Finish is invoked from its button's onClick, NOT as the form's submit
  // handler, and the button is deliberately `type="button"`. It used to be the
  // form's only `type="submit"`, which made it the browser's default button --
  // so pressing Enter anywhere in the form finished the retro. That was
  // harmless while the form held no text input, and became a real defect the
  // moment the add-action composer arrived: a household typed an action,
  // pressed Enter, and the retro was saved, finished and closed with the typed
  // body discarded. Nothing undoes that -- Discard is gated on a draft, the
  // server refuses DELETE on a finished retro, and there is no un-complete
  // route -- so finishing must never be something a stray keystroke can reach.
  async function finish() {
    // Explicit rather than leaning on the button's own `disabled`, so the
    // invariant holds for any future caller that does not go through it.
    if (actionsDisabled) return;
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

  return {
    mood,
    setMood,
    wentWell,
    setWentWell,
    wasHard,
    setWasHard,
    notes,
    setNotes,
    saveError,
    hadConflict,
    actionsDisabled,
    saveDraft,
    finish,
  };
}
