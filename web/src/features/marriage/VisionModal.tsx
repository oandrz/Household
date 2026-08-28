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
// this catch block runs, the hook's own onError (which sets it) has only
// been scheduled, not necessarily flushed -- deciding what to render here
// from that closure would risk exactly the staleness useRetro.ts's own
// comment warns any new caller about.
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
import { useEffect, useId, useState } from "react";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { Modal } from "../../components/Modal";
import { FieldPair } from "../../components/FieldPair";
import { useGoals } from "../money/useGoals";
import type { Goal } from "../money/goalSchemas";
import { VISION_COPY } from "./visionCopy";
import { currentVisionYear } from "./visionQueryKeys";
import type { useVision, SaveVisionBody } from "./useVision";

type DraftMeasure = {
  label: string;
  kind: "typed" | "linked";
  current: number;
  target: number;
  goalId: string;
};

type DraftPillar = {
  name: string;
  description: string;
  measures: DraftMeasure[];
};

type DraftMilestone = {
  year: number;
  title: string;
  note: string;
};

// A measure is typed OR linked, never both -- the domain refuses the
// ambiguous shape and so does the database's own measure_is_typed_or_linked.
// Switching modes therefore CLEARS the other mode's inputs rather than
// leaving them populated and hidden: a hidden value that still submits is
// how a form sends a body its own UI never showed anyone.
function setMeasureMode(measure: DraftMeasure, mode: "typed" | "linked"): DraftMeasure {
  return mode === "typed"
    ? { ...measure, kind: "typed", goalId: "", current: 0, target: 1 }
    : { ...measure, kind: "linked", goalId: "", current: 0, target: 0 };
}

function newMeasure(): DraftMeasure {
  return { label: "", kind: "typed", current: 0, target: 1, goalId: "" };
}

// Parses a bare, non-negative whole number typed into a plain text field --
// never NaN, which a raw `Number(event.target.value)` produces mid-edit (an
// empty field, a lone "-") and which would then sit in state as something
// `JSON.stringify` turns into `null` on the very next save. `type="text"
// inputMode="numeric"`, not `type="number"`, for the same reason every
// numeric field elsewhere in this codebase avoids it (formatMoney.ts's own
// convention) -- nothing here needs a spinner or the browser's own
// scientific-notation-accepting parser, and every value stays a plain JS
// number in state throughout, never a string re-parsed at save time.
function parseWholeNumber(raw: string): number {
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
}

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

function MeasureEditor({
  pillarIndex,
  measureIndex,
  measure,
  goals,
  onChange,
  onRemove,
}: {
  pillarIndex: number;
  measureIndex: number;
  measure: DraftMeasure;
  goals: Goal[];
  onChange: (measure: DraftMeasure) => void;
  onRemove: () => void;
}) {
  const idPrefix = `vision-modal-pillar-${pillarIndex}-measure-${measureIndex}`;
  return (
    <div data-testid="vision-modal-measure" className="flex flex-col gap-2 rounded-[10px] border border-hairline p-3">
      <div className="flex items-center gap-2">
        <div className="flex flex-1 flex-col gap-1">
          <label htmlFor={`${idPrefix}-label`} className="sr-only">
            {VISION_COPY.modalMeasureLabelLabel}
          </label>
          <input
            id={`${idPrefix}-label`}
            data-testid="vision-modal-measure-label"
            type="text"
            value={measure.label}
            placeholder={VISION_COPY.modalMeasureLabelLabel}
            onChange={(event) => onChange({ ...measure, label: event.target.value })}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
          />
        </div>
        <button
          type="button"
          data-testid="vision-modal-remove-measure"
          aria-label={VISION_COPY.removeMeasure(measure.label)}
          onClick={onRemove}
          className="flex h-11 w-11 flex-none items-center justify-center text-[15px] text-danger sm:h-7 sm:w-7"
        >
          ✕
        </button>
      </div>

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`${idPrefix}-mode`} className="text-[11px] font-semibold text-label">
          {VISION_COPY.modalMeasureModeLabel}
        </label>
        <select
          id={`${idPrefix}-mode`}
          data-testid="vision-modal-measure-mode"
          value={measure.kind}
          onChange={(event) => onChange(setMeasureMode(measure, event.target.value === "linked" ? "linked" : "typed"))}
          className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
        >
          <option value="typed">{VISION_COPY.modalMeasureModeTyped}</option>
          <option value="linked">{VISION_COPY.modalMeasureModeLinked}</option>
        </select>
      </div>

      {/* Only the fields the current mode actually uses are ever on screen
          -- setMeasureMode's own comment is the rule this renders; this is
          just the other half of it (no hidden twin sitting behind the one
          shown). */}
      {measure.kind === "typed" ? (
        <FieldPair>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${idPrefix}-current`} className="text-[11px] font-semibold text-label">
              {VISION_COPY.modalMeasureCurrentLabel}
            </label>
            <input
              id={`${idPrefix}-current`}
              data-testid="vision-modal-measure-current"
              type="text"
              inputMode="numeric"
              value={String(measure.current)}
              onChange={(event) => onChange({ ...measure, current: parseWholeNumber(event.target.value) })}
              className="tabular min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${idPrefix}-target`} className="text-[11px] font-semibold text-label">
              {VISION_COPY.modalMeasureTargetLabel}
            </label>
            <input
              id={`${idPrefix}-target`}
              data-testid="vision-modal-measure-target"
              type="text"
              inputMode="numeric"
              value={String(measure.target)}
              onChange={(event) => onChange({ ...measure, target: parseWholeNumber(event.target.value) })}
              className="tabular min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
            />
          </div>
        </FieldPair>
      ) : (
        <div className="flex flex-col gap-1.5">
          <label htmlFor={`${idPrefix}-goal`} className="text-[11px] font-semibold text-label">
            {VISION_COPY.modalMeasureGoalLabel}
          </label>
          {/* includeArchived: true -- decision 8 keeps an archived goal's
              own link and figure alive on the read side, so the picker that
              creates that link must be able to name one too; excluding
              archived goals here would also strand a measure already linked
              to one with no way for its option to render at all. */}
          <select
            id={`${idPrefix}-goal`}
            data-testid="vision-modal-measure-goal"
            value={measure.goalId}
            onChange={(event) => onChange({ ...measure, goalId: event.target.value })}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
          >
            <option value="">{VISION_COPY.modalMeasureGoalPlaceholder}</option>
            {goals.map((goal) => (
              <option key={goal.id} value={goal.id}>
                {goal.archivedAt ? `${goal.name} (archived)` : goal.name}
              </option>
            ))}
          </select>
        </div>
      )}
    </div>
  );
}

function PillarEditor({
  index,
  pillar,
  goals,
  onChange,
  onRemove,
}: {
  index: number;
  pillar: DraftPillar;
  goals: Goal[];
  onChange: (pillar: DraftPillar) => void;
  onRemove: () => void;
}) {
  const idPrefix = `vision-modal-pillar-${index}`;

  function updateMeasure(measureIndex: number, next: DraftMeasure) {
    onChange({ ...pillar, measures: pillar.measures.map((m, i) => (i === measureIndex ? next : m)) });
  }
  function removeMeasure(measureIndex: number) {
    onChange({ ...pillar, measures: pillar.measures.filter((_, i) => i !== measureIndex) });
  }
  function addMeasure() {
    onChange({ ...pillar, measures: [...pillar.measures, newMeasure()] });
  }

  return (
    <div data-testid="vision-modal-pillar" className="flex flex-col gap-3 rounded-xl border border-hairline p-4">
      <div className="flex items-start gap-2">
        <div className="flex flex-1 flex-col gap-1.5">
          <label htmlFor={`${idPrefix}-name`} className="text-xs font-semibold text-label">
            {VISION_COPY.modalPillarNameLabel}
          </label>
          <input
            id={`${idPrefix}-name`}
            data-testid="vision-modal-pillar-name"
            type="text"
            value={pillar.name}
            onChange={(event) => onChange({ ...pillar, name: event.target.value })}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13px] sm:min-h-0"
          />
        </div>
        <button
          type="button"
          data-testid="vision-modal-remove-pillar"
          aria-label={VISION_COPY.removePillar(pillar.name)}
          onClick={onRemove}
          className="mt-[22px] flex h-11 w-11 flex-none items-center justify-center text-[15px] text-danger sm:h-7 sm:w-7"
        >
          ✕
        </button>
      </div>

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`${idPrefix}-description`} className="text-xs font-semibold text-label">
          {VISION_COPY.modalPillarDescriptionLabel}
        </label>
        <textarea
          id={`${idPrefix}-description`}
          data-testid="vision-modal-pillar-description"
          value={pillar.description}
          onChange={(event) => onChange({ ...pillar, description: event.target.value })}
          rows={2}
          className="rounded-[10px] border border-hairline bg-card px-3.5 py-2.5 text-[13px] leading-relaxed"
        />
      </div>

      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-semibold uppercase tracking-[0.05em] text-muted">
            {VISION_COPY.modalMeasuresHeading}
          </span>
          <button
            type="button"
            data-testid="vision-modal-add-measure"
            onClick={addMeasure}
            className="text-[12px] font-semibold text-accent"
          >
            {VISION_COPY.addMeasure}
          </button>
        </div>
        {pillar.measures.map((measure, mi) => (
          // Index key: this array has no server id to key on either (a save
          // deletes and reinserts every child row -- visionSchemas.ts's own
          // comment), and add/remove here only ever appends or drops by
          // position, never reorders.
          <MeasureEditor
            key={mi}
            pillarIndex={index}
            measureIndex={mi}
            measure={measure}
            goals={goals}
            onChange={(next) => updateMeasure(mi, next)}
            onRemove={() => removeMeasure(mi)}
          />
        ))}
      </div>
    </div>
  );
}

function MilestoneEditor({
  index,
  milestone,
  onChange,
  onRemove,
}: {
  index: number;
  milestone: DraftMilestone;
  onChange: (milestone: DraftMilestone) => void;
  onRemove: () => void;
}) {
  const idPrefix = `vision-modal-milestone-${index}`;
  return (
    <div data-testid="vision-modal-milestone" className="flex items-start gap-2">
      <div className="flex w-20 flex-none flex-col gap-1">
        <label htmlFor={`${idPrefix}-year`} className="sr-only">
          {VISION_COPY.modalMilestoneYearLabel}
        </label>
        <input
          id={`${idPrefix}-year`}
          data-testid="vision-modal-milestone-year"
          type="text"
          inputMode="numeric"
          value={String(milestone.year)}
          onChange={(event) => onChange({ ...milestone, year: parseWholeNumber(event.target.value) })}
          className="tabular min-h-11 rounded-lg border border-hairline bg-card px-2 py-2 text-[13px] sm:min-h-0"
        />
      </div>
      <div className="flex flex-1 flex-col gap-1.5">
        <label htmlFor={`${idPrefix}-title`} className="sr-only">
          {VISION_COPY.modalMilestoneTitleLabel}
        </label>
        <input
          id={`${idPrefix}-title`}
          data-testid="vision-modal-milestone-title"
          type="text"
          placeholder={VISION_COPY.modalMilestoneTitleLabel}
          value={milestone.title}
          onChange={(event) => onChange({ ...milestone, title: event.target.value })}
          className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
        />
        <label htmlFor={`${idPrefix}-note`} className="sr-only">
          {VISION_COPY.modalMilestoneNoteLabel}
        </label>
        <input
          id={`${idPrefix}-note`}
          data-testid="vision-modal-milestone-note"
          type="text"
          placeholder={VISION_COPY.modalMilestoneNoteLabel}
          value={milestone.note}
          onChange={(event) => onChange({ ...milestone, note: event.target.value })}
          className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
        />
      </div>
      <button
        type="button"
        data-testid="vision-modal-remove-milestone"
        aria-label={VISION_COPY.removeMilestone(milestone.title)}
        onClick={onRemove}
        className="mt-0.5 flex h-11 w-11 flex-none items-center justify-center text-[15px] text-danger sm:h-7 sm:w-7"
      >
        ✕
      </button>
    </div>
  );
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

  const [theme, setTheme] = useState("");
  const [description, setDescription] = useState("");
  const [pillars, setPillars] = useState<DraftPillar[]>([]);
  const [milestones, setMilestones] = useState<DraftMilestone[]>([]);
  // Which year's document this draft was last seeded from -- null until the
  // first load. `data.year` is always present, even on the empty document
  // decision 9 returns for a year nobody has set (VisionService.Get's own
  // fallback carries `Year: year`, the year that was actually requested),
  // so this reseeds exactly once per year the household switches to and can
  // never seed one year's fields from another's data -- a simpler and more
  // robust key than a plain "have we ever seeded" boolean paired with its
  // own separate effect resetting it on every `year` change.
  const [seededYear, setSeededYear] = useState<number | null>(null);

  const [hadConflict, setHadConflict] = useState(false);
  const [isReloading, setIsReloading] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  useEffect(() => {
    if (!data || data.year === seededYear) return;
    setTheme(data.theme);
    setDescription(data.description);
    setPillars(
      data.pillars.map((p) => ({
        name: p.name,
        description: p.description,
        measures: p.measures.map((m) => ({
          label: m.label,
          // "broken" (a server-only kind -- measureKindSchema's own
          // comment) has nothing left to edit as either typed or linked: no
          // current/target the household ever set, and toMeasureView's own
          // default branch (api/internal/usecase/vision.go) never fills in
          // a goalId for this kind. Landing it as typed, with the same
          // blank-but-valid defaults setMeasureMode's own typed branch
          // uses, is the least surprising choice: an editable shape the
          // household must fill in themselves, not a silently resurrected
          // link to a goal that no longer resolves.
          kind: m.kind === "linked" ? "linked" : "typed",
          current: m.kind === "typed" ? m.current : 0,
          target: m.kind === "typed" ? m.target : m.kind === "broken" ? 1 : 0,
          goalId: m.kind === "linked" ? m.goalId : "",
        })),
      })),
    );
    setMilestones(data.milestones.map((m) => ({ year: m.year, title: m.title, note: m.note })));
    // A conflict or a leftover save error both describe the document THIS
    // draft was built against -- once a fresh one has just been seeded
    // (whether from the household's own Reload-and-discard, or simply
    // switching to a year that happens to already be cached), neither
    // means anything any more.
    setHadConflict(false);
    setSaveError(null);
    setSeededYear(data.year);
  }, [data, seededYear]);

  const themeId = useId();
  const yearSelectId = useId();
  const descriptionId = useId();

  const ready = !loading && !error && data !== undefined;

  function currentBody(): SaveVisionBody {
    return {
      theme,
      description,
      pillars: pillars.map((p) => ({
        name: p.name,
        description: p.description,
        measures: p.measures.map((m) => ({
          label: m.label,
          kind: m.kind,
          current: m.current,
          target: m.target,
          goalId: m.goalId,
        })),
      })),
      milestones: milestones.map((m) => ({ year: m.year, title: m.title, note: m.note })),
    };
  }

  async function handleSave() {
    setSaveError(null);
    // The one client-side check this modal makes before ever reaching the
    // server: the empty-document path (decision 9) seeds theme as "", so a
    // brand-new household's very first Save would otherwise round-trip a
    // 422 for something checkable in three lines. Hearth's own message, not
    // the browser's -- this form carries no `required` attribute anywhere,
    // per the UI-polish round's own rule that native validation is not this
    // product's error surface.
    if (theme.trim() === "") {
      setSaveError(VISION_COPY.modalThemeRequired);
      return;
    }
    try {
      await saveVision(currentBody());
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.code === "VISION_CHANGED") {
        setHadConflict(true);
      } else {
        setSaveError(apiErrorMessage(err, VISION_COPY.modalSaveError));
      }
    }
  }

  // Always closes, whether or not the refetch itself succeeded -- this
  // control's entire point is discarding the local draft, and there is
  // nothing left for a failed refetch to protect once that has happened.
  // Whatever the household sees next (the page behind this modal, or a
  // freshly reopened one) is what decides whether that refetch needs
  // retrying, not this button.
  async function handleReloadAndDiscard() {
    setIsReloading(true);
    try {
      await reload();
    } finally {
      setIsReloading(false);
      onClose();
    }
  }

  function addPillar() {
    setPillars((prev) => [...prev, { name: "", description: "", measures: [] }]);
  }
  function updatePillar(index: number, next: DraftPillar) {
    setPillars((prev) => prev.map((p, i) => (i === index ? next : p)));
  }
  function removePillar(index: number) {
    setPillars((prev) => prev.filter((_, i) => i !== index));
  }

  function addMilestone() {
    // Seeded with the vision's own year, not today's -- a longer-horizon
    // milestone is usually a few years out, but starting from the document
    // being edited is a closer guess than always defaulting to whatever
    // year the household happens to be looking at right now.
    setMilestones((prev) => [...prev, { year, title: "", note: "" }]);
  }
  function updateMilestone(index: number, next: DraftMilestone) {
    setMilestones((prev) => prev.map((m, i) => (i === index ? next : m)));
  }
  function removeMilestone(index: number) {
    setMilestones((prev) => prev.filter((_, i) => i !== index));
  }

  return (
    <Modal open onClose={onClose} title={VISION_COPY.modalTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">{VISION_COPY.modalPrivacyBadge}</p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          // The form submits nothing of its own -- RetroModal.tsx's own
          // header comment on its identical guard is the reason restated
          // here: this modal holds a pillar-name field, a milestone-title
          // field and more besides, any one of which implicitly submits on
          // Enter if left to the browser's own default button. Save vision
          // is `type="button"` below and reached only through its own
          // onClick.
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
          <div className="flex flex-col gap-1.5 sm:w-[150px]">
            <label htmlFor={yearSelectId} className="text-xs font-semibold text-label">
              {VISION_COPY.modalYearLabel}
            </label>
            <select
              id={yearSelectId}
              data-testid="vision-modal-year"
              value={year}
              onChange={(event) => onYearChange(Number(event.target.value))}
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            >
              {yearOptions().map((y) => (
                <option key={y} value={y}>
                  {y}
                </option>
              ))}
            </select>
          </div>

          {!ready ? (
            loading ? (
              <p className="text-xs text-muted">Loading…</p>
            ) : error ? (
              <p role="alert" data-testid="vision-modal-load-error" className="text-xs text-danger">
                {VISION_COPY.loadError}
              </p>
            ) : null
          ) : (
            <>
              <div className="flex flex-col gap-1.5">
                <label htmlFor={themeId} className="text-xs font-semibold text-label">
                  {VISION_COPY.modalThemeLabel}
                </label>
                <input
                  id={themeId}
                  data-testid="vision-modal-theme"
                  type="text"
                  value={theme}
                  onChange={(event) => setTheme(event.target.value)}
                  className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label htmlFor={descriptionId} className="text-xs font-semibold text-label">
                  {VISION_COPY.modalDescriptionLabel}
                </label>
                <textarea
                  id={descriptionId}
                  data-testid="vision-modal-description"
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  rows={3}
                  className="min-h-20 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed"
                />
              </div>

              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-label">{VISION_COPY.modalPillarsHeading}</span>
                  <button
                    type="button"
                    data-testid="vision-modal-add-pillar"
                    onClick={addPillar}
                    className="text-xs font-semibold text-accent"
                  >
                    {VISION_COPY.addPillar}
                  </button>
                </div>
                {pillars.map((pillar, pi) => (
                  // Index key: pillarDTO carries no id either (visionSchemas.ts's
                  // own comment), for the identical reason MeasureEditor's
                  // own list above uses one.
                  <PillarEditor
                    key={pi}
                    index={pi}
                    pillar={pillar}
                    goals={goals}
                    onChange={(next) => updatePillar(pi, next)}
                    onRemove={() => removePillar(pi)}
                  />
                ))}
              </div>

              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-label">{VISION_COPY.milestonesTitle}</span>
                  <button
                    type="button"
                    data-testid="vision-modal-add-milestone"
                    onClick={addMilestone}
                    className="text-xs font-semibold text-accent"
                  >
                    {VISION_COPY.addMilestone}
                  </button>
                </div>
                {milestones.map((milestone, mi) => (
                  <MilestoneEditor
                    key={mi}
                    index={mi}
                    milestone={milestone}
                    onChange={(next) => updateMilestone(mi, next)}
                    onRemove={() => removeMilestone(mi)}
                  />
                ))}
              </div>

              {/* Renders from `hadConflict`, the local one-way latch set
                  from the caught error's own `code` -- see this file's own
                  header comment for why, in full, and why its own action
                  calls `reload()` and then closes rather than trying to
                  resume this draft in place. */}
              {hadConflict && (
                <div
                  data-testid="vision-conflict"
                  role="alert"
                  className="flex flex-col gap-2 rounded-[10px] border border-hairline bg-callout p-3.5 text-[12.5px] leading-relaxed text-ink"
                >
                  <p>{VISION_COPY.conflictBanner}</p>
                  <button
                    type="button"
                    data-testid="vision-conflict-reload"
                    disabled={isReloading}
                    onClick={() => void handleReloadAndDiscard()}
                    className="min-h-11 self-start rounded-lg border border-hairline px-3 py-2.5 text-[12.5px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
                  >
                    {VISION_COPY.reloadAndDiscardChanges}
                  </button>
                </div>
              )}

              {saveError && !hadConflict && (
                <p role="alert" className="text-xs leading-snug text-danger">
                  {saveError}
                </p>
              )}
            </>
          )}
        </div>

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {VISION_COPY.cancel}
          </button>
          <button
            type="button"
            disabled={!ready || isSaving || hadConflict}
            onClick={() => void handleSave()}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {VISION_COPY.saveVision}
          </button>
        </div>
      </form>
    </Modal>
  );
}
