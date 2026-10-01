// One longer-horizon milestone in the Vision editor: its year, title and
// note. Moved out of VisionModal.tsx unchanged.
import { CloseIcon } from "../../components/icons";
import { VISION_COPY } from "./visionCopy";
import { parseWholeNumber, type DraftMilestone } from "./visionDraft";

export function MilestoneEditor({
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
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
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
        <CloseIcon />
      </button>
    </div>
  );
}
