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
// items-start, not the flex row's own default (stretch): a long label wraps
// to two or three lines at a phone width (a real browser walk at 305px
// found this, not assumed) and stretch would centre the short figure
// against that whole block instead of pinning it level with the label's
// first line. whitespace-nowrap on the figure span is the other half of
// that same walk's finding: without it, "2 of 4" itself wraps mid-number
// ("2 of" / "4") the moment the label crowds it for width -- flex-shrink-0
// keeps it from being asked to shrink into that wrap in the first place.
function MeasureRow({ measure }: { measure: VisionMeasure }) {
  if (!measure.hasFigure) {
    return (
      <div data-testid="vision-measure-row" className="flex items-start justify-between gap-3 text-[12.5px] text-ink">
        <span>{measure.label}</span>
        <span className="shrink-0 whitespace-nowrap text-muted">{VISION_COPY.measureFigureUnavailable}</span>
      </div>
    );
  }
  return (
    <div data-testid="vision-measure-row" className="flex items-start justify-between gap-3 text-[12.5px] text-ink">
      <span>{measure.label}</span>
      <span
        className={`tabular shrink-0 whitespace-nowrap ${measure.met ? "font-semibold text-accent" : "font-semibold"}`}
      >
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
      {/* The rule is part of the measures block, not a fixture of the card:
          a pillar with no measures rendered it anyway, so "Growth" drew a
          divider separating its description from nothing -- worst at a
          phone width, where the empty band below the rule is most of the
          card. Same rule the description above already follows: render the
          separator only when there is something to separate. A pillar
          legitimately has none (domain.Pillar caps measures at 8 but
          requires none), and the Overview's own line falls back to the
          pillar name for exactly this case. */}
      {pillar.measures.length > 0 && (
        <div className="mt-3.5 flex flex-col gap-2 border-t border-hairline pt-3">
          {pillar.measures.map((measure, i) => (
            // Index key: measureDTO carries no id at all (visionSchemas.ts),
            // and this array is read-only display here -- nothing in this
            // task reorders or removes a single measure client-side.
            <MeasureRow key={i} measure={measure} />
          ))}
        </div>
      )}
    </div>
  );
}
