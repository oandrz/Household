// The Start/Edit retro modal (design/Household Dashboard.dc.html's
// `modalRetro` panel): the mood picker, the two textareas, the money
// check-in panel, the carry-over offer, the add-action composer, and Save
// draft / Finish retro. Built on `components/Modal` the same way every
// other feature modal is (GoalModal.tsx's own shape is the closest
// precedent: this component calls `useRetro(month)` itself rather than
// taking a mutation as a prop, because nothing upstream has already
// resolved one).
//
// The composer is a fix-round addition, not part of Task 14's own tests:
// this modal shipped with the carry-over offer as its actions block's only
// caller of `addAction`, which meant a household could carry last month's
// unfinished action forward but never write a brand-new one -- the exact
// shape docs/LEARNING.md pattern 15 names (a feature fully built and tested
// one layer down with no screen that can reach it). Fixed here rather than
// left for Task 17's browser walk to rediscover.
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
import { MoneyCheckInPanel } from "./MoneyCheckInPanel";
import { RETRO_COPY, monthYearLabel, previousMonthName } from "./retroCopy";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { useRetro, type SaveRetroBody } from "./useRetro";
import type { RetroAction } from "./retroSchemas";

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
  // own. Filtered to owners only: marriage is an owner-only capability
  // (CLAUDE.md's own note, "a limited member can never hold CapMarriage
  // today"), so a retro action's assignee is always one of the two parents,
  // never a child membership.
  const members = useHouseholdMembers();

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

  // Which of `carryOver`'s own action ids this mount has already posted a
  // carry for -- purely a local, per-mount safeguard against a double click,
  // NOT something the server tracks. Confirmed by reading the query behind
  // OpenInMonth (retro.sql's own ListOpenActionsInMonth): it excludes an
  // action only once ITS OWN done_at is set, and carrying one never touches
  // July's own row (design decision 4: "July's own row is untouched and
  // stays unticked") -- there is no server-side de-dup, and no unique
  // constraint on carried_from either (migrations/00009_retros.sql), so the
  // same July action would be offered again forever without this. A fresh
  // mount (closing and reopening this modal) starts this set empty again,
  // same as `hadConflict`'s own "fresh mount, fresh everything" contract --
  // the offer reappearing after a reload is the accepted, honest behaviour,
  // not a bug this is trying to hide.
  const [carriedIds, setCarriedIds] = useState<Set<string>>(new Set());
  const [carryingId, setCarryingId] = useState<string | null>(null);
  const [carryError, setCarryError] = useState<string | null>(null);

  // The add-action composer's own local state -- a brand-new action, never
  // a carry (see handleAddAction's own comment on why `carriedFrom` is never
  // sent here). Reset to blank after a successful post so the household can
  // add a second action right away without reopening anything -- the exact
  // flow Task 17's browser-walk criterion 7 exercises ("Add two actions,
  // assign one to each partner and one to both").
  const [newActionBody, setNewActionBody] = useState("");
  const [newActionAssigneeIds, setNewActionAssigneeIds] = useState<Set<string>>(new Set());
  const [isAddingAction, setIsAddingAction] = useState(false);
  const [addActionError, setAddActionError] = useState<string | null>(null);

  // Discard draft's own local state -- TransactionModal.tsx's own
  // confirmingDelete/isDeleting pair, the same shape (one item to delete,
  // not a list, so a single boolean rather than GoalContributionsPanel.tsx's
  // per-row `confirmingId`/`deletingId`). Confirmation happens in the page,
  // never `window.confirm` -- this codebase's own established convention.
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);
  const [isDiscarding, setIsDiscarding] = useState(false);
  const [discardError, setDiscardError] = useState<string | null>(null);

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
    // The submit button carries `disabled={actionsDisabled}`, which already
    // blocks both a click and a browser's own implicit-submit-on-Enter (a
    // disabled default button is excluded from implicit form submission per
    // spec) -- but that makes "never finish while disabled" incidental to
    // HTML's own button rules rather than something this function enforces
    // itself. Explicit here so the invariant holds even if a future caller
    // ever invokes handleFinish some other way (a keyboard shortcut, a test
    // calling the handler directly) that does not go through the button.
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

  // Carries one of last month's still-open actions onto THIS retro. `action`
  // is always an element of `retro.data.carryOver` -- never a freehand id --
  // because the only caller is the map below, which only ever hands this
  // function an entry from that same list. useRetro.ts's own
  // AddRetroActionBody doc comment names this exact trust boundary: passing
  // anything else would silently break RetroActionRow's "Carried from
  // {month}" label, which infers the source month by calendar arithmetic
  // rather than reading it off the wire (retroActionDTO carries no month for
  // a carried-from id at all).
  //
  // Checked against `actionsDisabled` up front, the same explicit-invariant
  // reason `handleFinish` checks it rather than leaning on the button's own
  // `disabled` attribute alone -- once `hadConflict` latches, nothing in
  // this modal should still be writing against a retro this tab's own
  // understanding of may already be stale.
  async function handleCarryOver(action: RetroAction) {
    if (actionsDisabled) return;
    setCarryError(null);
    setCarryingId(action.id);
    try {
      await retro.addAction({ body: action.body, carriedFrom: action.id });
      setCarriedIds((prev) => new Set(prev).add(action.id));
    } catch (err) {
      setCarryError(apiErrorMessage(err, RETRO_COPY.carryOverError));
    } finally {
      setCarryingId(null);
    }
  }

  function toggleAssignee(memberId: string) {
    setNewActionAssigneeIds((prev) => {
      const next = new Set(prev);
      if (next.has(memberId)) {
        next.delete(memberId);
      } else {
        next.add(memberId);
      }
      return next;
    });
  }

  // Posts a brand-new action -- deliberately no `carriedFrom` at all, not
  // even `""`: useRetro.ts's own AddRetroActionBody defaults an omitted
  // `carriedFrom` to `""` on the wire itself, so this function has nothing
  // to pass either way, and never should. `carriedFrom` is handleCarryOver's
  // field alone (its own comment states the trust boundary: only ever an id
  // taken from `retro.data.carryOver`), and a composer that also set it
  // would be exactly the freehand-id risk that comment warns against.
  //
  // The Add control's own `disabled` already keeps this unreachable with a
  // blank body (see the button below), but the check is repeated here for
  // the same reason handleFinish repeats `actionsDisabled` -- an invariant
  // this function owns, not one left incidental to a button's own attribute.
  async function handleAddAction() {
    const body = newActionBody.trim();
    if (body === "" || actionsDisabled) return;
    setAddActionError(null);
    setIsAddingAction(true);
    try {
      await retro.addAction({ body, assigneeMembershipIds: Array.from(newActionAssigneeIds) });
      setNewActionBody("");
      setNewActionAssigneeIds(new Set());
    } catch (err) {
      setAddActionError(apiErrorMessage(err, RETRO_COPY.addActionError));
    } finally {
      setIsAddingAction(false);
    }
  }

  // Deletes the retro outright -- only ever reachable while it is still a
  // draft (the trigger button's own `completedAt === null` gate below), but
  // checked against `actionsDisabled` too, the same explicit-invariant
  // reason handleFinish/handleCarryOver/handleAddAction each repeat it: once
  // `hadConflict` latches, nothing in this modal should still be writing
  // against a retro this tab's own understanding of may already be stale.
  // TransactionModal.tsx's own `handleDelete` is the precedent this mirrors:
  // clear any prior error, mark in flight, await the mutation, close on
  // success, and collapse BOTH `isDiscarding` and `confirmingDiscard` back
  // to their resting state in `finally` regardless of outcome -- a failed
  // delete should not leave the confirm/cancel pair stuck open forever.
  async function handleDiscardDraft() {
    if (actionsDisabled) return;
    setDiscardError(null);
    setIsDiscarding(true);
    try {
      await retro.discardDraft();
      onDiscarded?.();
      onClose();
    } catch (err) {
      setDiscardError(apiErrorMessage(err, RETRO_COPY.discardDraftError));
    } finally {
      setIsDiscarding(false);
      setConfirmingDiscard(false);
    }
  }

  // Disables both actions below for two different reasons: a write already
  // in flight (transient), or `hadConflict` (permanent for this mount --
  // enabling them again on the strength of `retro.conflict` alone, which
  // clears on any successful refetch including one this modal never asked
  // for, is the exact gap the latch exists to close; see its own comment).
  const actionsDisabled = isSaving || isFinishing || hadConflict;

  // The household's owners, in the order useHouseholdMembers itself returns
  // them -- filtered here rather than a second fetch (useHouseholdMembers.ts's
  // own header comment: the point of that hook is one shared cache entry).
  // `member.role` is a plain string on the wire (memberSchema's own
  // `z.string()`, not an enum) -- "owner" is the same literal
  // AccountModal.tsx/MembersPanel.tsx already filter and compare against.
  const owners = (members.data ?? []).filter((member) => member.role === "owner");

  // Computed once here, outside the loading/error ternary below, so `?.`/`??`
  // do the "not loaded yet" handling rather than a type-narrowing dance
  // inside JSX. Filtered by `carriedIds` BEFORE the "is there anything to
  // show" check further down -- gating that check on the server's own
  // unfiltered `carryOver.length` instead would leave the "Still open from
  // July" heading on screen with an empty list under it once every row in
  // this mount has been carried, the exact "heading with nothing under it"
  // shape this screen's own BulletCard/actions-list guards elsewhere all
  // refuse to render.
  const openCarryOver = retro.data?.carryOver.filter((action) => !carriedIds.has(action.id)) ?? [];

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

          <div data-testid="retro-modal-money-mount">
            <MoneyCheckInPanel month={month} />
          </div>

          {/* The carry-over offer (design decision 4): last month's still-
              open actions, one row each, offered only while there is
              something left to offer -- an empty `openCarryOver` renders
              nothing at all here, not an empty heading over a blank list
              (the same "no placeholder for an absence" rule the rest of
              this screen already follows). */}
          <div data-testid="retro-modal-actions-mount" className="flex flex-col gap-2">
            {openCarryOver.length > 0 && (
              <>
                <h3 className="text-xs font-semibold text-label">
                  {RETRO_COPY.carryOverHeading(previousMonthName(month))}
                </h3>
                <div className="flex flex-col gap-2">
                  {openCarryOver.map((action) => (
                    <div
                      key={action.id}
                      className="flex items-center justify-between gap-3 rounded-[10px] border border-hairline px-3.5 py-2.5"
                    >
                      <span className="text-[13px] text-ink">{action.body}</span>
                      <button
                        type="button"
                        aria-label={RETRO_COPY.carryOverButton(action.body)}
                        disabled={actionsDisabled || carryingId === action.id}
                        onClick={() => void handleCarryOver(action)}
                        className="min-h-11 flex-none rounded-lg border border-hairline px-3 text-[12px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
                      >
                        {RETRO_COPY.carryOverButtonLabel}
                      </button>
                    </div>
                  ))}
                </div>
                {carryError && (
                  <p role="alert" className="text-xs leading-snug text-danger">
                    {carryError}
                  </p>
                )}
              </>
            )}

            {/* The add-action composer -- a body input, one toggle per
                household owner (none, one or both may be selected; RetroActionRow.tsx's
                own rule already renders zero assignees as no initials rather
                than a placeholder, so nothing here forces a choice), and an
                Add control. `data-testid` pins this block's own wiring: an
                assertion that finds this testid, or the input/Add button
                inside it, would go red if this block's render were ever
                deleted from the modal -- the same "prove the parent still
                renders the child" shape Tasks 11-13 each had to add after
                review. */}
            <div
              data-testid="retro-add-action"
              className="flex flex-col gap-2 rounded-[10px] border border-dashed border-hairline p-3.5"
            >
              <input
                type="text"
                value={newActionBody}
                onChange={(event) => setNewActionBody(event.target.value)}
                placeholder={RETRO_COPY.addActionPlaceholder}
                disabled={actionsDisabled}
                className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13px] sm:min-h-0"
              />
              <div className="flex flex-wrap items-center gap-2">
                {owners.map((owner) => {
                  const selected = newActionAssigneeIds.has(owner.id);
                  return (
                    <button
                      key={owner.id}
                      type="button"
                      aria-label={RETRO_COPY.assignToMember(owner.user.displayName)}
                      aria-pressed={selected}
                      disabled={actionsDisabled}
                      onClick={() => toggleAssignee(owner.id)}
                      // Review finding: a hard `sm:h-[26px] sm:w-[26px]`
                      // clamp shrank this below the 44px floor on every
                      // viewport >=640px, including desktop, and was never
                      // measured or named as an exception -- the only
                      // `sm:h-[Npx]` on an interactive element anywhere in
                      // features/marriage, features/money or components.
                      // `min-h-11 ... sm:min-h-0` is the house pattern every
                      // other control here uses (RetroActionRow.tsx's own
                      // checkbox label, every button in this file): it
                      // removes the floor rather than clamping below it, so
                      // padding decides the size at `sm` the same way it
                      // does everywhere else. `aspect-square` keeps this
                      // circular at both sizes without a second, separate
                      // width utility to keep in sync with the height one.
                      className={`flex aspect-square min-h-11 flex-none items-center justify-center rounded-full border p-1.5 text-[13px] font-semibold disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 ${
                        selected ? "border-accent bg-callout text-accent" : "border-hairline text-label"
                      }`}
                    >
                      {owner.user.avatarInitial}
                    </button>
                  );
                })}
                <button
                  type="button"
                  disabled={actionsDisabled || isAddingAction || newActionBody.trim() === ""}
                  onClick={() => void handleAddAction()}
                  className="min-h-11 ml-auto flex-none rounded-lg bg-accent px-4 text-[12px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
                >
                  {RETRO_COPY.addAction}
                </button>
              </div>
              {addActionError && (
                <p role="alert" className="text-xs leading-snug text-danger">
                  {addActionError}
                </p>
              )}
            </div>
          </div>

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

          {/* Discard draft (design decision 2: "a draft can be deleted; a
              finished retro cannot"). Rendered only for a draft
              (`completedAt === null`) -- a finished retro offers nothing,
              because the server refuses the delete (retro_handlers.go's own
              `WHERE completed_at IS NULL`) and an offer that always fails is
              worse than no offer. TransactionModal.tsx's own delete flow is
              the shape this mirrors: confirmation happens in this page, not
              `window.confirm`, and its own bordered box sits apart from
              Save draft/Finish retro below so no one reaches for it by
              accident. `data-testid` pins this block's own wiring the same
              way retro-add-action does -- an assertion that finds it, or
              the trigger/confirm controls inside it, would go red if this
              block's render were ever deleted from the modal. */}
          {retro.data.retro.completedAt === null && (
            <div data-testid="retro-discard-draft" className="rounded-[10px] border border-hairline p-3">
              {confirmingDiscard ? (
                <div className="flex flex-col gap-2.5">
                  <p className="text-[12.5px] text-ink">{RETRO_COPY.discardDraftConfirmBody}</p>
                  <div className="flex gap-2.5">
                    <button
                      type="button"
                      onClick={() => setConfirmingDiscard(false)}
                      className="min-h-11 flex-1 rounded-lg border border-hairline py-2 text-center text-[13px] font-semibold text-label sm:min-h-0"
                    >
                      {RETRO_COPY.discardDraftCancelAction}
                    </button>
                    <button
                      type="button"
                      disabled={isDiscarding || actionsDisabled}
                      onClick={() => void handleDiscardDraft()}
                      className="min-h-11 flex-1 rounded-lg bg-danger py-2 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
                    >
                      {RETRO_COPY.discardDraftConfirmAction}
                    </button>
                  </div>
                  {discardError && (
                    <p role="alert" className="text-xs leading-snug text-danger">
                      {discardError}
                    </p>
                  )}
                </div>
              ) : (
                <button
                  type="button"
                  disabled={actionsDisabled}
                  onClick={() => setConfirmingDiscard(true)}
                  className="min-h-11 text-[13px] font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
                >
                  {RETRO_COPY.discardDraftTrigger}
                </button>
              )}
            </div>
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
