// Overview's "Vision 2026" card (design/Household Dashboard.dc.html, lines
// 336-338: a solid green box, "This year's theme", the quote, and three
// flat commitment lines beneath it -- onClick="{{ go_vision }}" on the whole
// box, not a separate CTA inside it).
//
// The design's own three lines -- "1 weekend away per quarter", "Date
// night twice a month", "Screens off by 9:30pm" -- are neither pillar names
// ("Us before logistics") nor measure labels ("Date nights / month"). They
// are a third shape the design never says how to store, so this card does
// not attempt to reproduce them literally (spec decision 3). It renders one
// line per pillar instead, in `position` order, each showing that pillar's
// FIRST measure with its live figures -- "first" is free, because measures
// already carry a display order for the pillar cards (PillarCard.tsx renders
// them in array order too), so nothing here invents a "show on overview"
// flag, and there is no "which three of six" question to answer.
//
// Owns its own useVision(currentVisionYear()) call rather than taking a
// prop from OverviewPage, the same NextRetroCard.tsx/NextBillCard.tsx shape
// -- and for the identical reason NextRetroCard.tsx's own header comment
// gives for useRetros: useVision (marriage/useVision.ts) takes no `enabled`
// option, so a component that must not fire this request for a member
// without `marriage` has to be the thing OverviewPage chooses not to mount
// at all, not a hook OverviewPage calls unconditionally and hopes not to
// use. visionQueryKeys.ts's own header comment anticipates exactly this:
// "Overview's VisionCard reads the same year's vision as VisionPage does" --
// two independently-mounted callers sharing one cache entry by key, neither
// importing the other.
import { Link } from "@tanstack/react-router";
import { currentVisionYear } from "../marriage/visionQueryKeys";
import { useVision } from "../marriage/useVision";
import type { VisionPillar } from "../marriage/visionSchemas";
import { OVERVIEW_COPY } from "./copy";

// One line per pillar, each its FIRST measure -- see this file's own header
// comment for why that diverges from the design's own three flat lines
// (spec decision 3). A pillar with no measures at all falls back to its own
// name, so the line still says something rather than rendering empty.
//
// The hasFigure check runs BEFORE the kind check, not after: hasFigure
// false covers three server states at once (measureDTO's own comment,
// restated in PillarCard.tsx's MeasureRow) -- a deleted linked goal, an
// unresolved link, or a kind this build doesn't recognise ("broken"). Any
// of those reaching the kind branch below would print a stale or invented
// percent/count, which is exactly the untrue-number ruling 3 forbids.
function overviewLine(pillar: VisionPillar): { label: string; figure: string | null } {
  const measure = pillar.measures[0];
  if (!measure) return { label: pillar.name, figure: null };
  if (!measure.hasFigure) return { label: measure.label, figure: null };
  return {
    label: measure.label,
    figure: measure.kind === "linked" ? `${measure.percent}%` : `${measure.current} of ${measure.target}`,
  };
}

export function VisionCard() {
  const vision = useVision(currentVisionYear());

  // Covers "still loading" and "errored" in one guard, the same
  // NextRetroCard.tsx convention: this card has no loading spinner or error
  // region of its own, only a glance-figure or nothing. GET
  // /marriage/vision is marriage-AND-owner gated (router.go), but a member
  // reaching this card at all already holds `marriage`, and today that
  // capability cannot be held without also being the owner
  // (domain.ErrLimitedCannotHoldMarriage) -- so a routine 403 is not a real
  // path here, and this guard does not need to distinguish one from an
  // ordinary in-flight state, the same call NextRetroCard.tsx's own header
  // comment makes for GET /retros.
  if (!vision.data) return null;

  const { data } = vision;
  // version 0 means the year has no vision yet (visionSchema's own
  // comment) -- omitted entirely (ruling 2), not rendered as an empty
  // quotation. Overview's setup checklist is the surface that tells a
  // household something is missing; this card claiming a blank vision
  // exists would be worse than saying nothing.
  if (data.version === 0) return null;

  return (
    <Link
      to="/marriage/vision"
      data-testid="vision-card"
      className="block rounded-xl bg-accent p-[22px] text-white"
    >
      <h2 className="text-[12px] uppercase tracking-[0.08em] text-white/70">
        {OVERVIEW_COPY.visionThemeLabel(data.year)}
      </h2>
      <p className="mt-1.5 text-[19px] font-semibold tracking-[-0.01em]">
        {OVERVIEW_COPY.visionThemeQuote(data.theme)}
      </p>
      {data.pillars.length > 0 && (
        <div className="mt-4 flex flex-col gap-2.5 text-[12.5px]">
          {data.pillars.map((pillar, i) => {
            const line = overviewLine(pillar);
            return (
              // Index key: pillarDTO carries no id (visionSchemas.ts), and
              // this array is read-only display here -- PillarCard.tsx's
              // own identical choice for the same reason.
              <div
                key={i}
                data-testid="vision-overview-line"
                className="flex items-baseline justify-between gap-3 text-white/85"
              >
                <span>{line.label}</span>
                {/* tabular: index.css's own font-variant-numeric utility --
                    a figure that changes month to month should not shift
                    its neighbours' width as its digits do. */}
                {line.figure && (
                  <span className="tabular shrink-0 whitespace-nowrap font-semibold text-white">
                    {line.figure}
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Link>
  );
}
