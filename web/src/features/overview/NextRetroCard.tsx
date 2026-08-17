// Overview's "Next retro" tile (design/Household Dashboard.dc.html's own
// go_retros-linked card): the current month's retro if the household has
// started one -- draft or finished -- else a prompt to start it. The
// design's mockup shows a SCHEDULED retro with a countdown ("in 8 days"),
// but this product has no scheduling concept for a retro -- a retro is
// created the moment "Start retro" is clicked (handleStartRetro's own
// comment: the server picks the month, there is no calendar entry for a
// future one) -- so this card answers a different, real question instead:
// has this month's check-in been started, and is there anything still on
// it.
//
// Owns its own useRetros() call, the same shape NextBillCard.tsx uses for
// useBills -- but unlike that card, this one takes no `enabled` prop of its
// own. NextBillCard needs one because OverviewPage mounts it unconditionally
// for every owner (Task 16's own pinned "renders nothing while its query is
// disabled" behaviour). Retros has no such requirement to reach, and
// useRetros() (Task 9) was not given an `enabled` option to begin with --
// this task's own file list does not touch useRetros.ts. So the gate lives
// one level up instead: OverviewPage.tsx only mounts <NextRetroCard /> at
// all for a member whose capabilities include "marriage". A component that
// is never mounted never calls the hook inside it, which is what "gate the
// hook call itself, not just the render" (the brief's own words) actually
// requires here -- there is no `enabled: false` idle state for this
// component to sit in.
import { Link } from "@tanstack/react-router";
import { currentMonth } from "../money/month";
import { monthNameOnly, nextMonthName } from "../marriage/retroCopy";
import { useRetros } from "../marriage/useRetros";
import { OVERVIEW_COPY } from "./copy";

export function NextRetroCard() {
  const retros = useRetros();

  // Same three-states-as-one guard NextBillCard.tsx uses: still loading, or
  // errored (a household owner is the only caller this route usually sees,
  // per router.go's own marriage+owner guard, but this card has no error
  // region of its own to show one in -- retros.error is not read here) --
  // neither has a figure to show, and this card's only job is a glance or
  // nothing, never a spinner or an error competing for space with the cards
  // beside it.
  if (!retros.data) return null;

  const { data } = retros;
  // "Current" here is literally this calendar month, not `data.startMonth`
  // -- those answer different questions. A retro already exists for this
  // month whenever `retros` (newest-month-first, retro_repo.go's own
  // ORDER BY) carries a row for it; `startMonth` only ever names a month
  // with NO retro yet, and StartableMonth (domain/retro.go) can point at
  // last month instead of this one when a couple is finishing last month's
  // retro in the first few days of a new one. Reading `data.startMonth`
  // directly in the empty branch below (rather than re-deriving it here)
  // is what keeps that priority correct without duplicating it.
  const current = data.retros.find((r) => r.month === currentMonth());

  return (
    <section
      aria-labelledby="overview-next-retro-heading"
      data-testid="next-retro-card"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="overview-next-retro-heading" className="text-xs text-muted">
        {OVERVIEW_COPY.nextRetroHeading}
      </h2>

      {current ? (
        <>
          <p className="mt-1.5 text-[15px] font-semibold text-ink">
            {OVERVIEW_COPY.nextRetroTitle(monthNameOnly(current.month))}
          </p>
          {/* Draft-only: a finished retro has nothing left to flag as
              unfinished. RetroHistoryList.tsx's own draftInProgress row is
              the same signal, restated for this card. */}
          {!current.finished && (
            <p className="mt-1 text-[11.5px] font-semibold text-accent">{OVERVIEW_COPY.nextRetroInProgress}</p>
          )}
          {/* The design's "carried from June retro" section under Next
              retro -- this card cannot show WHICH actions are still open
              (nextRetroActions' own comment explains why), so it shows the
              honest count it does have instead of a per-action list this
              response does not carry. Omitted at zero, never "0 actions". */}
          {current.actionCount > 0 && (
            <p className="mt-1 text-[11.5px] text-muted">
              {OVERVIEW_COPY.nextRetroActions(current.actionCount, nextMonthName(current.month))}
            </p>
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
    </section>
  );
}
