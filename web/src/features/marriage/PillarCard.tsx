// One pillar of the household's Vision: its ordinal label, name,
// description and measures. The measure row is where the figureless rule
// lives -- kept in exactly one place (MeasureRow below) so no second call
// site can drift from it.
import { VISION_COPY } from "./visionCopy";
import type { VisionMeasure, VisionPillar } from "./visionSchemas";

// A measure with no figure renders its label and a short explanation, never
// a number -- not "0 of 0", not "0%". `hasFigure: false` is what the server
// sends for three states (measureDTO's own comment, vision_handlers.go): a
// linked goal that was deleted, a link that failed to resolve, or a kind
// this build doesn't recognise. Rendering a number for any of those would
// state something untrue about the household, the same way a zero net
// worth would after a primary-currency change leaves it uncomputable.
function MeasureRow({ measure }: { measure: VisionMeasure }) {
  if (!measure.hasFigure) {
    return (
      <div data-testid="vision-measure-row" className="flex justify-between gap-3 text-[12.5px] text-ink">
        <span>{measure.label}</span>
        <span className="text-muted">{VISION_COPY.measureFigureUnavailable}</span>
      </div>
    );
  }
  return (
    <div data-testid="vision-measure-row" className="flex justify-between gap-3 text-[12.5px] text-ink">
      <span>{measure.label}</span>
      <span className={`tabular ${measure.met ? "font-semibold text-accent" : "font-semibold"}`}>
        {measure.kind === "linked" ? `${measure.percent}%` : `${measure.current} of ${measure.target}`}
        {measure.met ? " ✓" : ""}
      </span>
    </div>
  );
}

// `index` is the pillar's 0-based array position; VISION_COPY.pillarLabel
// wants a 1-based ordinal -- this is the one place that translation
// happens, so a caller passes the raw index straight through.
export function PillarCard({ pillar, index }: { pillar: VisionPillar; index: number }) {
  return (
    <div data-testid="vision-pillar-card" className="rounded-xl border border-hairline bg-card p-[22px]">
      <div className="text-[12px] uppercase tracking-[0.07em] text-muted">
        {VISION_COPY.pillarLabel(index + 1)}
      </div>
      <div className="mt-2 text-[15px] font-semibold text-ink">{pillar.name}</div>
      {/* An empty pillar description renders nothing, not an empty <p> --
          toDomainVision (vision_handlers.go) always sends "" rather than
          omitting the field, so this checks truthiness rather than trusting
          a pillar always has one worth a line (task-11's own ruling 4). */}
      {pillar.description && (
        <p data-testid="vision-pillar-description" className="mt-1.5 text-[12.5px] leading-[1.55] text-muted">
          {pillar.description}
        </p>
      )}
      <div className="mt-3.5 flex flex-col gap-2 border-t border-hairline pt-3">
        {pillar.measures.map((measure, i) => (
          // Index key: measureDTO carries no id at all (visionSchemas.ts),
          // and this array is read-only display here -- nothing in this
          // task reorders or removes a single measure client-side.
          <MeasureRow key={i} measure={measure} />
        ))}
      </div>
    </div>
  );
}
