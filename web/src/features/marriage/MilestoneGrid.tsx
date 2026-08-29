// The "Longer horizon" panel: one card per milestone plus the dashed
// "+ Add milestone" affordance. `onEdit` is a plain callback owned by
// VisionPage.tsx -- it opens the real Edit-vision modal (Vision spec's
// task 12), the same one the header's own Edit vision button calls,
// matching the design's two entry points into one editor (dc.html: both
// carry onClick="{{ openVision }}").
import { VISION_COPY } from "./visionCopy";
import type { VisionMilestone } from "./visionSchemas";

function MilestoneCard({ milestone }: { milestone: VisionMilestone }) {
  return (
    <div data-testid="vision-milestone-card" className="rounded-[10px] border border-hairline p-4">
      <div className="tabular text-[12px] font-semibold text-accent">{milestone.year}</div>
      <div className="mt-1.5 text-[13.5px] font-semibold text-ink">{milestone.title}</div>
      {/* A milestone's own note is "" on the wire whenever a household
          didn't write one -- the same "empty string renders nothing" rule
          the vision's own description and each pillar's description
          follow (task-11's own ruling 4). */}
      {milestone.note && (
        <div data-testid="vision-milestone-note" className="mt-1 text-[12px] text-muted">
          {milestone.note}
        </div>
      )}
    </div>
  );
}

export function MilestoneGrid({
  milestones,
  onEdit,
}: {
  milestones: VisionMilestone[];
  onEdit: () => void;
}) {
  return (
    <div className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 className="mb-4 text-sm font-semibold text-ink">{VISION_COPY.milestonesTitle}</h2>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-4">
        {milestones.map((milestone, i) => (
          // Index key: milestoneDTO carries no id (visionSchemas.ts), and
          // this array is read-only display here.
          <MilestoneCard key={i} milestone={milestone} />
        ))}
        <button
          type="button"
          data-testid="vision-add-milestone"
          onClick={onEdit}
          className="min-h-11 rounded-[10px] border border-dashed border-hairline p-4 text-[13px] text-muted transition-colors duration-[var(--transition-state)] hover:border-accent hover:text-accent"
        >
          {VISION_COPY.addMilestone}
        </button>
      </div>
    </div>
  );
}
