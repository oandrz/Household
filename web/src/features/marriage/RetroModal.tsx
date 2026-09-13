// The Start/Edit retro modal (design/Household Dashboard.dc.html's
// `modalRetro` panel): the mood picker, the two textareas, the money
// check-in panel, the carry-over offer, the add-action composer, and Save
// draft / Finish retro. Built on `components/Modal` the same way every
// other feature modal is (GoalModal.tsx's own shape is the closest
// precedent: this component calls `useRetro(month)` itself rather than
// taking a mutation as a prop, because nothing upstream has already
// resolved one).
//
// This file only composes. The draft and its two writes live in
// useRetroDraft.ts; the carry-over offer, the add-action composer and
// Discard draft are CarryOverList.tsx, AddActionComposer.tsx and
// DiscardDraftControl.tsx, each owning its own state.
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
import { useId } from "react";
import { Field } from "../../components/Field";
import { FieldPair } from "../../components/FieldPair";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { AddActionComposer } from "./AddActionComposer";
import { CarryOverList } from "./CarryOverList";
import { DiscardDraftControl } from "./DiscardDraftControl";
import { MoneyCheckInPanel } from "./MoneyCheckInPanel";
import { RETRO_COPY, monthYearLabel } from "./retroCopy";
import { useRetro } from "./useRetro";
import { useRetroDraft } from "./useRetroDraft";

export function RetroModal({
  month,
  onClose,
  onDiscarded,
}: {
  month: string;
  onClose: () => void;
  // Fired only on a successful discard, before `onClose` -- a real browser
  // walk against this exact flow found the gap this closes: RetrosPage.tsx's
  // own `selectedMonth` is a SEPARATE piece of state from this modal's
  // `month`/`onClose`, and closing the modal alone leaves RetroDetail.tsx
  // behind it still pointed at a month whose retro no longer exists, which
  // renders as a visible "Couldn't load this retro." error the instant this
  // modal closes on an otherwise successful delete. Optional so every
  // existing call site (Save draft, Finish retro, and every RetroModal.test.tsx
  // render that only ever passes `onClose`) is unaffected -- only the
  // discard path has anything to tell the page.
  onDiscarded?: () => void;
}) {
  const retro = useRetro(month);
  // The same members query RetroDetail.tsx/RetroActionRow.tsx already share
  // for assignee initials (useHouseholdMembers.ts's own header comment: one
  // cache entry, not a fourth private copy) -- not a second fetch of its
  // own. Called here, when the modal mounts, rather than inside
  // AddActionComposer: the composer only mounts once the retro has loaded,
  // and starting the members fetch that late would leave its owner toggles
  // missing for a moment after the composer first appears. Filtered to owners
  // inside the composer: marriage is an owner-only capability (CLAUDE.md's
  // own note, "a limited member can never hold CapMarriage today"), so a
  // retro action's assignee is always one of the two parents, never a child
  // membership.
  const members = useHouseholdMembers();
  const draft = useRetroDraft(retro, onClose);

  const moodLegendId = useId();
  const wentWellId = useId();
  const wasHardId = useId();
  const notesId = useId();

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
        <form
          className="flex flex-col gap-4"
          onSubmit={(event) => {
            // The form submits nothing. A form with a single text input
            // implicitly submits on Enter, and every control in here is a
            // state transition -- finishing a retro cannot be undone, so no
            // keystroke may reach it by accident. The composer handles Enter
            // itself (see its own onKeyDown) because adding the action is what
            // the person meant; everywhere else Enter does nothing at all.
            event.preventDefault();
          }}
        >
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
                const selected = draft.mood === option.value;
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
                      onChange={() => draft.setMood(option.value)}
                      aria-label={option.label}
                      className="h-4 w-4 accent-accent"
                    />
                  </label>
                );
              })}
            </div>
          </div>

          <FieldPair>
            <Field label={RETRO_COPY.wentWellHeading} htmlFor={wentWellId} labelTone="accent">
              <textarea
                id={wentWellId}
                value={draft.wentWell}
                onChange={(event) => draft.setWentWell(event.target.value)}
                placeholder={RETRO_COPY.wentWellPlaceholder}
                rows={4}
                className="min-h-24 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
              />
            </Field>
            <Field label={RETRO_COPY.wasHardHeading} htmlFor={wasHardId} labelTone="danger">
              <textarea
                id={wasHardId}
                value={draft.wasHard}
                onChange={(event) => draft.setWasHard(event.target.value)}
                placeholder={RETRO_COPY.wasHardPlaceholder}
                rows={4}
                className="min-h-24 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
              />
            </Field>
          </FieldPair>

          <Field label={RETRO_COPY.notesHeading} htmlFor={notesId}>
            <textarea
              id={notesId}
              value={draft.notes}
              onChange={(event) => draft.setNotes(event.target.value)}
              placeholder={RETRO_COPY.notesPlaceholder}
              rows={3}
              className="min-h-20 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
            />
          </Field>

          <div data-testid="retro-modal-money-mount">
            <MoneyCheckInPanel month={month} />
          </div>

          <div data-testid="retro-modal-actions-mount" className="flex flex-col gap-2">
            <CarryOverList
              month={month}
              carryOver={retro.data.carryOver}
              addAction={retro.addAction}
              disabled={draft.actionsDisabled}
            />
            <AddActionComposer
              members={members.data ?? []}
              addAction={retro.addAction}
              disabled={draft.actionsDisabled}
            />
          </div>

          {/* Renders from `hadConflict`, the local one-way latch, NOT
              `retro.conflict` -- see hadConflict's own comment in
              useRetroDraft.ts on why a two-way flag that can clear itself out
              from under this modal (a background refetch-on-window-focus, for
              instance) must never be what decides whether this banner -- or
              the buttons below it -- are showing. No Reload control here on
              purpose; conflictBanner's own comment has the
              browser-walk-verified reason one was tried and removed. */}
          {draft.hadConflict && (
            <div
              data-testid="retro-conflict"
              role="alert"
              className="flex flex-col gap-2 rounded-[10px] border border-hairline bg-callout p-3.5 text-[12.5px] leading-relaxed text-ink"
            >
              <p>{RETRO_COPY.conflictBanner}</p>
            </div>
          )}

          {draft.saveError && (
            <p role="alert" className="text-xs leading-snug text-danger">
              {draft.saveError}
            </p>
          )}

          {/* Rendered only for a draft (`completedAt === null`) -- a finished
              retro offers nothing, because the server refuses the delete
              (retro_handlers.go's own `WHERE completed_at IS NULL`) and an
              offer that always fails is worse than no offer. */}
          {retro.data.retro.completedAt === null && (
            <DiscardDraftControl
              disabled={draft.actionsDisabled}
              discardDraft={retro.discardDraft}
              onDiscarded={onDiscarded}
              onClose={onClose}
            />
          )}

          {/* primaryType="button", not "submit" -- see finish's comment in
              useRetroDraft.ts. As the form's default button Finish was
              reachable by Enter from anywhere in the form, including the
              composer and the mood picker, and finishing a retro cannot be
              undone. */}
          <ModalActions
            secondaryLabel={RETRO_COPY.saveDraft}
            onSecondary={() => void draft.saveDraft()}
            secondaryDisabled={draft.actionsDisabled}
            primaryLabel={RETRO_COPY.finishRetro}
            primaryType="button"
            onPrimary={() => void draft.finish()}
            primaryDisabled={draft.actionsDisabled}
          />
        </form>
      )}
    </Modal>
  );
}
