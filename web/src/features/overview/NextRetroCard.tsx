// Overview's "Next retro" tile (design/Household Dashboard.dc.html's own
// go_retros-linked card). It answers one question: what is there to do about
// retros right now. In order:
//
//   1. this month's retro, if the household has started one, draft or
//      finished;
//   2. else last month's retro, if it is still a draft: an unfinished retro
//      is the thing to finish, and a prompt to start the new month would
//      step over it. This is what the card shows on the 1st of a month
//      while last month's retro is still open;
//   3. else a prompt to start the month the server says can be started.
//
// "This month" and "last month" are the household's months (ADR 12).
//
// A draft shown here carries a link to the Retros page, where it is listed
// as in progress and can be opened. The page may still offer to start the
// new month beside it. That is not a disagreement: the card names the one
// thing that needs attention, and the page offers everything that can be
// done.
//
// The design's mockup shows a SCHEDULED retro with a countdown ("in 8
// days"), but this product has no scheduling concept for a retro: one is
// created the moment "Start retro" is clicked, and the server picks the
// month. So there is no countdown here.
//
// Owns its own useRetros() call, the same shape NextBillCard.tsx uses for
// useBills, but takes no `enabled` prop: useRetros() has no such option.
// The gate lives one level up instead. OverviewPage.tsx only mounts
// <NextRetroCard /> for a member whose capabilities include "marriage", and
// a component that is never mounted never calls the hook inside it, so a
// member without marriage never fires GET /retros at all.
import { Link } from "@tanstack/react-router";
import { useHouseholdZone } from "../auth/useHouseholdZone";
import { monthBefore, monthIn, yearIn } from "../../lib/householdDate";
import { monthNameOnly, nextMonthName } from "../marriage/retroCopy";
import { useRetros } from "../marriage/useRetros";
import { useVision } from "../marriage/useVision";
import { OVERVIEW_COPY } from "./copy";

export function NextRetroCard() {
  const retros = useRetros();
  // The check-in strip's own data (design's "Vision check-in: 2026 theme —
  // 'Slow down together'", drawn inside this same card). A second,
  // independent useVision call for the household's year rather than one shared
  // with VisionCard.tsx -- VisionCard.tsx's own header comment explains why
  // that hook has to be mounted per-component (no `enabled` option to gate
  // it centrally) and why two independent callers on the same query key cost
  // one request, not two.
  const zone = useHouseholdZone();
  const vision = useVision(yearIn(zone));

  // Same three-states-as-one guard NextBillCard.tsx uses: still loading, or
  // errored (a household owner is the only caller this route usually sees,
  // per router.go's own marriage+owner guard, but this card has no error
  // region of its own to show one in -- retros.error is not read here) --
  // neither has a figure to show, and this card's only job is a glance or
  // nothing, never a spinner or an error competing for space with the cards
  // beside it.
  if (!retros.data) return null;

  const { data } = retros;
  // Both lookups read the list, never `data.startMonth`: startMonth only
  // ever names a month with NO retro yet, and the two months looked for here
  // are ones that have one.
  const thisMonth = monthIn(zone);
  const current = data.retros.find((r) => r.month === thisMonth);
  // Last month's retro counts only while it is a draft, and only last
  // month's: an older draft is not brought forward. Don't widen this to "any
  // open draft": the Retros page offers last month to start in that case
  // (domain.StartableMonth), and the card would then name a different month
  // from the page's button.
  const lastMonthDraft = data.retros.find((r) => r.month === monthBefore(thisMonth) && !r.finished);
  const shown = current ?? lastMonthDraft;

  return (
    <section
      aria-labelledby="overview-next-retro-heading"
      data-testid="next-retro-card"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="overview-next-retro-heading" className="text-xs text-muted">
        {OVERVIEW_COPY.nextRetroHeading}
      </h2>

      {shown ? (
        <>
          <p className="mt-1.5 text-[15px] font-semibold text-ink">
            {OVERVIEW_COPY.nextRetroTitle(monthNameOnly(shown.month))}
          </p>
          {/* Draft-only: a finished retro has nothing left to flag as
              unfinished. RetroHistoryList.tsx's own draftInProgress row is
              the same signal, restated for this card. */}
          {!shown.finished && (
            <p className="mt-1 text-[11.5px] font-semibold text-accent">{OVERVIEW_COPY.nextRetroInProgress}</p>
          )}
          {/* The design's "carried from June retro" section under Next
              retro -- this card still cannot show WHICH actions are open, a
              per-action list that only exists behind a second request
              (nextRetroActions' own comment explains why), but it shows the
              OPEN count, not the total: retroSummarySchema.openActionCount,
              never actionCount, or a fully-ticked retro would still read as
              outstanding work here. Omitted at zero, never "0 actions". */}
          {shown.openActionCount > 0 && (
            <p className="mt-1 text-[11.5px] text-muted">
              {OVERVIEW_COPY.nextRetroActions(shown.openActionCount, nextMonthName(shown.month))}
            </p>
          )}
          {/* The way to the draft. Without it the card would name an
              unfinished retro and offer no way to reach it. Same link
              classes as the prompt below. */}
          {!shown.finished && (
            <Link
              to="/marriage/retros"
              className="mt-3 inline-flex min-h-11 items-center text-[13px] font-semibold text-accent sm:min-h-0"
            >
              {OVERVIEW_COPY.nextRetroContinue(monthNameOnly(shown.month))}
            </Link>
          )}
        </>
      ) : (
        <>
          <p className="mt-1.5 text-[15px] text-ink">{OVERVIEW_COPY.nextRetroNone}</p>
          {/* inline-flex items-center min-h-11 sm:min-h-0: BudgetCard.tsx's
              own comment on this identical pattern has the reason. */}
          <Link
            to="/marriage/retros"
            className="mt-3 inline-flex min-h-11 items-center text-[13px] font-semibold text-accent sm:min-h-0"
          >
            {data.startMonth ? OVERVIEW_COPY.nextRetroStart(monthNameOnly(data.startMonth)) : OVERVIEW_COPY.nextRetroGo}
          </Link>
        </>
      )}

      {/* The design's own strip (dc.html: a border-topped line under the
          carried-actions block, inside this same card). Gated on
          `vision.data?.theme` truthiness alone, not a second `version === 0`
          check: a year with no vision always carries theme: "" on the wire
          (visionSchema's own comment), so an empty theme already covers
          "still loading," "errored" and "version 0" in the one condition a
          reader can see is right, without a second source of truth that
          could silently disagree with VisionCard.tsx's own version check. */}
      {vision.data?.theme && (
        <p
          data-testid="vision-checkin-strip"
          className="mt-3.5 border-t border-hairline pt-3.5 text-[12.5px] text-muted"
        >
          {OVERVIEW_COPY.visionCheckInLabel}{" "}
          <b className="font-semibold text-ink">
            {OVERVIEW_COPY.visionCheckInTheme(vision.data.year, vision.data.theme)}
          </b>
        </p>
      )}
    </section>
  );
}
