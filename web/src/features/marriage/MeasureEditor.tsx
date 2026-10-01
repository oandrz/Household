// One measure inside a pillar, in the Vision editor: its label, whether it is
// a typed figure or linked to a savings goal, and only the fields that mode
// uses. Moved out of VisionModal.tsx unchanged.
import { FieldPair } from "../../components/FieldPair";
import { CloseIcon } from "../../components/icons";
import type { Goal } from "../money/goalSchemas";
import { VISION_COPY } from "./visionCopy";
import { parseWholeNumber, setMeasureMode, type DraftMeasure } from "./visionDraft";

export function MeasureEditor({
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
        <div className="flex min-w-0 flex-1 flex-col gap-1">
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
          <CloseIcon />
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
