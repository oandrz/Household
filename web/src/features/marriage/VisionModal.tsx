// The whole-document Vision editor (design/Household Dashboard.dc.html's
// `modalVision` panel) -- theme, year, description, every pillar (name,
// description, measures) and every milestone, saved in one PUT. Follows
// RetroModal.tsx for structure and focus management (the latter inherited
// free from Modal.tsx) and the in-page confirmation convention -- though
// nothing here needs that last one: removing a pillar/measure/milestone row
// only edits this modal's own local draft, the same reversible, not-yet-sent
// state Cancel already discards wholesale, so it needs no more ceremony than
// the design's own plain ✕ (dc.html draws no confirm step for any of them
// either).
//
// This file only composes. The draft, its seed effect, Save and
// Reload-and-discard live in useVisionDraft.ts; each pillar, measure and
// milestone row is PillarEditor.tsx, MeasureEditor.tsx and
// MilestoneEditor.tsx.
//
// Takes `year`/`onYearChange`/the pieces of `useVision(year)` this modal
// needs as props, rather than calling the hook itself the way RetroModal
// calls its own useRetro(month) -- useVision.ts's own header comment is
// explicit about why: VisionPage owns `year` as its own state, and this
// modal's year select must change it on that SAME mounted useVision
// instance rather than a second one of its own, or `conflictAt` (a plain
// useState local to whichever call site owns it) would not survive a year
// switch the way that file's own effect is written to guarantee.
// VisionPage.tsx's own header comment says as much from its side: "Task 12
// modifies this file anyway to mount the modal, and adds the setter at the
// same time it adds the first caller for it."
//
// ---- The year select stays live through a year switch's own fetch -------
//
// The select is rendered outside the "is this year's document loaded yet"
// gate below, not inside it. Putting it inside would mean the one control
// that just caused a load disappears the moment it fires -- a household that
// picked the wrong year could not change its mind until the wrong one
// finished loading first.
//
// ---- The conflict mechanism (a deliberate departure from RetroModal) ----
//
// useRetro.ts:109-116 records that RetroModal reads neither `conflict` nor
// `reload()`: a 409 is decided from `err.code` alone, and the banner offers
// no action, because `reload()` only clears that flag -- it never touches
// RetroModal's own local fields, so re-enabling Save after it would resend
// whatever stale text was still sitting in the form against the fresh
// version it just fetched. useVision.test.ts's own comment on its "reload
// clears conflict" test says plainly that this task owns whether, and how,
// VisionModal uses `conflict`/`reload` -- it is not settled by precedent
// either way, and the identical "the modal needs this" assumption is on
// record as having been wrong once already, for retros.
//
// This modal still detects the conflict from `err.code`, for the same
// reason RetroModal does: `conflict` (the hook's own derived flag) is a
// value closed over from the render that started the save, and by the time
// useVisionDraft's catch block runs, the hook's own onError (which sets it)
// has only been scheduled, not necessarily flushed -- deciding what to
// render here from that closure would risk exactly the staleness
// useRetro.ts's own comment warns any new caller about.
//
// Where this genuinely departs from RetroModal is the action offered once a
// conflict latches. RetroModal offers none, because there is no safe way to
// make `reload()` alone resume that modal in place. Here, the control calls
// `reload()` and then closes the modal outright -- discarding this draft
// rather than leaving it on screen behind a banner that a later, unrelated
// background refetch could clear out from under it. That closes the exact
// gap `reload()` alone can't: once it fires, there is no "next Save" left
// that could ever resubmit stale fields against a version this tab never
// saw. The button is named for that outcome (`reloadAndDiscardChanges`), not
// called a bare "Reload" -- a control that silently discards a household's
// edits does not get a friendly, ambiguous label.
import { useId } from "react";
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { useGoals } from "../money/useGoals";
import { MilestoneEditor } from "./MilestoneEditor";
import { PillarEditor } from "./PillarEditor";
import type { useVision } from "./useVision";
import { useVisionDraft } from "./useVisionDraft";
import { VISION_COPY } from "./visionCopy";
import { currentVisionYear } from "./visionQueryKeys";

// The previous, current and next calendar year, anchored on TODAY
// (currentVisionYear()) rather than on whichever year is currently loaded --
// the spec's own reasoning: a household setting January's theme in December
// needs next year, one writing up a year they never recorded needs last
// year, and nothing in the design asks for 2019. Anchoring on today rather
// than on `year` also means a household that picks "next year" and reopens
// this select later still sees the same three options, not a window that
// keeps sliding with their own pick.
function yearOptions(): number[] {
  const current = currentVisionYear();
  return [current - 1, current, current + 1];
}

export function VisionModal({
  year,
  onYearChange,
  data,
  loading,
  error,
  saveVision,
  isSaving,
  reload,
  onClose,
}: {
  year: number;
  onYearChange: (year: number) => void;
  data: ReturnType<typeof useVision>["data"];
  loading: ReturnType<typeof useVision>["loading"];
  error: ReturnType<typeof useVision>["error"];
  saveVision: ReturnType<typeof useVision>["saveVision"];
  isSaving: ReturnType<typeof useVision>["isSaving"];
  reload: ReturnType<typeof useVision>["reload"];
  onClose: () => void;
}) {
  // includeArchived: true -- see MeasureEditor's own comment on its goal
  // select for why the picker needs every goal, not only live ones.
  const goalsQuery = useGoals({ includeArchived: true });
  const goals = goalsQuery.data?.goals ?? [];

  const draft = useVisionDraft({ year, data, saveVision, reload, onClose });

  const themeId = useId();
  const yearSelectId = useId();
  const descriptionId = useId();

  const ready = !loading && !error && data !== undefined;

  return (
    // wide: the design draws this modal at 640px (dc.html:928), not the 420px
    // every other modal takes. It is the only form in the app nesting three
    // levels of editable rows -- pillars, each pillar's own measures, then
    // milestones -- and at 420px each nested card and its ✕ crowd the field
    // beside them.
    <Modal open wide onClose={onClose} title={VISION_COPY.modalTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">{VISION_COPY.modalPrivacyBadge}</p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          // The form submits nothing of its own -- RetroModal.tsx's own
          // header comment on its identical guard is the reason restated
          // here: this modal holds a pillar-name field, a milestone-title
          // field and more besides, any one of which implicitly submits on
          // Enter if left to the browser's own default button. Save vision
          // is primaryType="button" below and reached only through its own
          // click.
          event.preventDefault();
        }}
      >
        {/* max-h/overflow-y-auto scoped to this content block, not the
            shared Modal.tsx: that component's own panel is a fixed 420px
            wide box with no max-height or scrolling of its own (every
            existing caller's content fits comfortably inside one screen).
            A vision with even two or three pillars, each carrying a
            description and a couple of measures, plus a handful of
            milestones, does not -- the design's own modalVision block sets
            `max-height:88%;overflow-y:auto` on itself for exactly this.
            Cancel/Save stay outside this box, pinned below it, rather than
            scrolling away with the rest -- an improvement on the design's
            own single scrolling region, not a rebuild of Modal.tsx for one
            caller's content length. */}
        <div className="flex max-h-[65vh] flex-col gap-4 overflow-y-auto pr-1">
          {/* Theme and year share one row, theme wide and year narrow, the
              way the design draws them (dc.html:932, grid 1fr/150px). The
              theme is what this modal is for; the year is which one you are
              setting. Stacking the year alone above the theme -- which is
              what this was -- gave the smaller decision the more prominent
              position, and put it first in the tab order too.

              The year select stays mounted through a year change while the
              newly chosen year's document loads; the theme field cannot,
              because there is no theme to edit until it arrives. So the
              left cell carries whichever of the two states applies, and the
              row keeps its shape either way rather than collapsing to a
              lone select with a loading line orphaned beneath it. Below
              `sm` the grid is one column and they stack, theme first. */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_150px]">
            <div className="flex flex-col justify-end gap-1.5">
              {ready ? (
                <>
                  <label htmlFor={themeId} className="text-xs font-semibold text-label">
                    {VISION_COPY.modalThemeLabel}
                  </label>
                  <input
                    id={themeId}
                    data-testid="vision-modal-theme"
                    type="text"
                    value={draft.theme}
                    onChange={(event) => draft.setTheme(event.target.value)}
                    className={FIELD_CONTROL_CLASS}
                  />
                </>
              ) : loading ? (
                <p className="text-xs text-muted">Loading…</p>
              ) : error ? (
                <p role="alert" data-testid="vision-modal-load-error" className="text-xs text-danger">
                  {VISION_COPY.loadError}
                </p>
              ) : null}
            </div>

            <Field label={VISION_COPY.modalYearLabel} htmlFor={yearSelectId}>
              <select
                id={yearSelectId}
                data-testid="vision-modal-year"
                value={year}
                onChange={(event) => onYearChange(Number(event.target.value))}
                className={FIELD_CONTROL_CLASS}
              >
                {yearOptions().map((y) => (
                  <option key={y} value={y}>
                    {y}
                  </option>
                ))}
              </select>
            </Field>
          </div>

          {ready && (
            <>
              <Field label={VISION_COPY.modalDescriptionLabel} htmlFor={descriptionId}>
                <textarea
                  id={descriptionId}
                  data-testid="vision-modal-description"
                  value={draft.description}
                  onChange={(event) => draft.setDescription(event.target.value)}
                  rows={3}
                  className="min-h-20 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
                />
              </Field>

              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-label">{VISION_COPY.modalPillarsHeading}</span>
                  <button
                    type="button"
                    data-testid="vision-modal-add-pillar"
                    onClick={draft.addPillar}
                    className="text-xs font-semibold text-accent"
                  >
                    {VISION_COPY.addPillar}
                  </button>
                </div>
                {draft.pillars.map((pillar, pi) => (
                  // Index key: pillarDTO carries no id either (visionSchemas.ts's
                  // own comment), for the identical reason MeasureEditor's
                  // own list in PillarEditor.tsx uses one.
                  <PillarEditor
                    key={pi}
                    index={pi}
                    pillar={pillar}
                    goals={goals}
                    onChange={(next) => draft.updatePillar(pi, next)}
                    onRemove={() => draft.removePillar(pi)}
                  />
                ))}
              </div>

              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-label">{VISION_COPY.milestonesTitle}</span>
                  <button
                    type="button"
                    data-testid="vision-modal-add-milestone"
                    onClick={draft.addMilestone}
                    className="text-xs font-semibold text-accent"
                  >
                    {VISION_COPY.addMilestone}
                  </button>
                </div>
                {draft.milestones.map((milestone, mi) => (
                  <MilestoneEditor
                    key={mi}
                    index={mi}
                    milestone={milestone}
                    onChange={(next) => draft.updateMilestone(mi, next)}
                    onRemove={() => draft.removeMilestone(mi)}
                  />
                ))}
              </div>

              {/* Renders from `hadConflict`, the local one-way latch set
                  from the caught error's own `code` -- see this file's own
                  header comment for why, in full, and why its own action
                  calls `reload()` and then closes rather than trying to
                  resume this draft in place. */}
              {draft.hadConflict && (
                <div
                  data-testid="vision-conflict"
                  role="alert"
                  className="flex flex-col gap-2 rounded-[10px] border border-hairline bg-callout p-3.5 text-[12.5px] leading-relaxed text-ink"
                >
                  <p>{VISION_COPY.conflictBanner}</p>
                  <button
                    type="button"
                    data-testid="vision-conflict-reload"
                    disabled={draft.isReloading}
                    onClick={() => void draft.handleReloadAndDiscard()}
                    className="min-h-11 self-start rounded-lg border border-hairline px-3 py-2.5 text-[12.5px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
                  >
                    {VISION_COPY.reloadAndDiscardChanges}
                  </button>
                </div>
              )}

              {draft.saveError && !draft.hadConflict && (
                <p role="alert" className="text-xs leading-snug text-danger">
                  {draft.saveError}
                </p>
              )}
            </>
          )}
        </div>

        <ModalActions
          secondaryLabel={VISION_COPY.cancel}
          onSecondary={onClose}
          primaryLabel={VISION_COPY.saveVision}
          primaryType="button"
          onPrimary={() => void draft.handleSave()}
          primaryDisabled={!ready || isSaving || draft.hadConflict}
        />
      </form>
    </Modal>
  );
}
