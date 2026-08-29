// The Vision & goals screen: header (title, subtitle, Edit vision button),
// a green theme hero, a three-column pillar grid and the "Longer horizon"
// milestone panel. Composition only, the GoalsPage.tsx/RetrosPage.tsx
// convention: fetch orchestration lives in useVision.ts, and no apiFetch
// call belongs here.
//
// Owns `year` AND its setter (task-11's own ruling 1): the Edit-vision
// modal's own year select changes which year this page -- and the single
// mounted `useVision(year)` call below -- looks at, rather than each
// holding a year of its own. useVision.ts's own header comment explains why
// that matters, not just where the state lives: switching year on this SAME
// instance is what lets its `conflictAt` effect (keyed on `year`) reset
// itself; a second, modal-owned `useVision` call would carry a second,
// independent `conflictAt` that effect could never reach.
import { useState } from "react";
import { ApiError } from "../../api/client";
import { PageContainer } from "../../components/PageContainer";
import { MilestoneGrid } from "./MilestoneGrid";
import { PillarCard } from "./PillarCard";
import { VisionModal } from "./VisionModal";
import { VISION_COPY } from "./visionCopy";
import { currentVisionYear } from "./visionQueryKeys";
import { useVision } from "./useVision";

export function VisionPage() {
  const [year, setYear] = useState(currentVisionYear());
  const vision = useVision(year);
  // Whether the Edit-vision modal is open -- a plain boolean rather than
  // the modal's own component instance, since it takes no id or other prop
  // this page would otherwise need to remember between opens (unlike
  // RetrosPage.tsx's own editingMonth, which the modal needs to know WHICH
  // retro to load). Every one of this modal's three entry points -- the
  // header's Edit vision button, MilestoneGrid's own "+ Add milestone"
  // tile, and the empty state's own call to action -- opens the identical
  // editor against whichever `year` this page currently holds. The design
  // itself only draws two of these (dc.html: the Edit vision button and the
  // "+ Add milestone" tile both carry onClick="{{ openVision }}", lines 595
  // and 612) -- the empty state's own call to action is Hearth's own third
  // entry point, not drawn in the design at all.
  const [modalOpen, setModalOpen] = useState(false);

  function onEdit() {
    setModalOpen(true);
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

      {/* Mounted last, matching RetrosPage.tsx's own RetroModal placement --
          rendered only while open, so a closed modal costs this page
          nothing (no GET, no goals fetch) beyond the boolean itself. */}
      {modalOpen && (
        <VisionModal
          year={year}
          onYearChange={setYear}
          data={vision.data}
          loading={vision.loading}
          error={vision.error}
          saveVision={vision.saveVision}
          isSaving={vision.isSaving}
          reload={vision.reload}
          onClose={() => setModalOpen(false)}
        />
      )}
    </PageContainer>
  );
}
