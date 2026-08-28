// The Vision & goals screen: header (title, subtitle, Edit vision button),
// a green theme hero, a three-column pillar grid and the "Longer horizon"
// milestone panel. Composition only, the GoalsPage.tsx/RetrosPage.tsx
// convention: fetch orchestration lives in useVision.ts, and no apiFetch
// call belongs here.
//
// Owns `year` (initialised to currentVisionYear()) because Task 12's modal
// will offer a year select that changes which year this page looks at
// (task-11's own ruling 1) -- this task does not build that control, only
// the state it will later write to, so there is no setter here yet: Task
// 12 modifies this file anyway to mount the modal, and adds the setter at
// the same time it adds the first caller for it. `onEdit` is likewise a
// placeholder Task 12 replaces wholesale (ruling 2): building half a modal
// here would mean reviewing it twice.
import { useState } from "react";
import { ApiError } from "../../api/client";
import { PageContainer } from "../../components/PageContainer";
import { MilestoneGrid } from "./MilestoneGrid";
import { PillarCard } from "./PillarCard";
import { VISION_COPY } from "./visionCopy";
import { currentVisionYear } from "./visionQueryKeys";
import { useVision } from "./useVision";

export function VisionPage() {
  const [year] = useState(currentVisionYear());
  const vision = useVision(year);

  // Task 12 replaces this with real state that opens VisionModal -- both
  // the header's own Edit vision button below and MilestoneGrid's own
  // "+ Add milestone" tile call it (ruling 2), matching the design's own
  // two entry points into one editor (dc.html: both carry
  // onClick="{{ openVision }}").
  function onEdit() {
    // No-op until Task 12.
  }

  if (vision.loading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }

  if (vision.error) {
    // GET /marriage/vision is marriage-AND-owner gated (router.go's own
    // comment on the group), the identical shape GET /retros carries
    // (RetrosPage.tsx's own branch). Branching on the real status, not a
    // second useMe() role check -- a role check here would be a second
    // source of truth that could disagree with what the server actually
    // decided. This is the exact gap BillsPage.tsx shipped without
    // (docs/LEARNING.md pattern 1), found again in BudgetPage.tsx and
    // TransactionsPage.tsx -- GoalsPage.tsx/RetrosPage.tsx already carry
    // the fix, restated here: a routine 403 is not a server failure and
    // must not get the same red alert.
    const status = vision.error instanceof ApiError ? vision.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="vision-owner-only" className="m-9 rounded-xl border border-hairline bg-card p-[22px]">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{VISION_COPY.title}</h1>
          <h2 className="mt-4 text-xs text-muted">{VISION_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{VISION_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="vision-load-error" className="p-9 text-xs text-danger">
        {VISION_COPY.loadError}
      </p>
    );
  }

  // vision.error is null and vision.loading is false here, so data is
  // present -- TanStack Query's own contract, the same defensive guard
  // GoalsPage.tsx/RetrosPage.tsx use for their own `!data` branch.
  if (!vision.data) {
    return null;
  }

  const data = vision.data;
  // version 0 means the year has no vision yet (visionSchema's own
  // comment) -- rendered as an invitation to set one, not a grid of cards
  // with nothing in them.
  const isEmpty = data.version === 0;

  return (
    <PageContainer data-testid="vision-page">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{VISION_COPY.title}</h1>
          <p className="mt-1 text-[13px] text-muted">{VISION_COPY.subtitle}</p>
        </div>
        <button
          type="button"
          data-testid="vision-edit"
          onClick={onEdit}
          className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white sm:min-h-0"
        >
          {VISION_COPY.editVision}
        </button>
      </div>

      {isEmpty ? (
        <div data-testid="vision-empty-state" className="rounded-xl border border-hairline bg-card p-16 text-center">
          <div className="text-[19px] font-semibold tracking-[-0.01em] text-ink">
            {VISION_COPY.emptyHeadline(year)}
          </div>
          <p className="mx-auto mt-2 max-w-[420px] text-[13.5px] leading-relaxed text-muted">
            {VISION_COPY.emptyBody}
          </p>
          <div className="mt-6 flex justify-center">
            <button
              type="button"
              data-testid="vision-empty-cta"
              onClick={onEdit}
              className="min-h-11 rounded-lg bg-accent px-5 py-2.5 text-[13px] font-semibold text-white sm:min-h-0"
            >
              {VISION_COPY.emptyCta}
            </button>
          </div>
        </div>
      ) : (
        <>
          {/* No marriage-duration block -- spec decision 2. The design's
              own hero (dc.html) splits theme/description on the left from
              "Married · 14 years · Feb 14, 2012" on the right; that block
              is drawn but deliberately not built here, so the hero renders
              full width instead of split. */}
          <div data-testid="vision-hero" className="rounded-[14px] bg-accent px-8 py-[30px] text-white">
            <div className="text-[12px] uppercase tracking-[0.08em] text-white/70">
              {VISION_COPY.themeLabel(data.year)}
            </div>
            <div className="mt-2 text-[30px] font-semibold tracking-[-0.02em]">
              {VISION_COPY.themeQuote(data.theme)}
            </div>
            {/* An empty vision description renders nothing, not an empty
                block -- the empty-vision response carries description: ""
                on the wire (visionSchema's own comment), and a household
                with a theme but no written-out description can hit this
                too, not only version 0 (task-11's own ruling 4). */}
            {data.description && (
              <p data-testid="vision-hero-description" className="mt-2 max-w-[560px] text-[13.5px] leading-relaxed text-white/85">
                {data.description}
              </p>
            )}
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {data.pillars.map((pillar, i) => (
              // Index key: pillarDTO carries no id (visionSchemas.ts), and
              // this array is read-only display here.
              <PillarCard key={i} pillar={pillar} index={i} />
            ))}
          </div>

          <MilestoneGrid milestones={data.milestones} onEdit={onEdit} />
        </>
      )}
    </PageContainer>
  );
}
