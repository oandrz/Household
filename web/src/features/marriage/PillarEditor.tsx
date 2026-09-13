// One pillar in the Vision editor: its name, its description, and its list of
// measures. Moved out of VisionModal.tsx unchanged.
import { Field } from "../../components/Field";
import { CloseIcon } from "../../components/icons";
import type { Goal } from "../money/goalSchemas";
import { MeasureEditor } from "./MeasureEditor";
import { VISION_COPY } from "./visionCopy";
import { newMeasure, type DraftMeasure, type DraftPillar } from "./visionDraft";

export function PillarEditor({
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
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
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
          <CloseIcon />
        </button>
      </div>

      <Field label={VISION_COPY.modalPillarDescriptionLabel} htmlFor={`${idPrefix}-description`}>
        <textarea
          id={`${idPrefix}-description`}
          data-testid="vision-modal-pillar-description"
          value={pillar.description}
          onChange={(event) => onChange({ ...pillar, description: event.target.value })}
          rows={2}
          className="rounded-[10px] border border-hairline bg-card px-3.5 py-2.5 text-[13px] leading-relaxed"
        />
      </Field>

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
