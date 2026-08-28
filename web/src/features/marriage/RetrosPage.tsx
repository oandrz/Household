// The Retros screen: header (title, subtitle, done-count clause, privacy
// badge, start button), the five states from the task-10 brief, and the
// mount points Tasks 11-13 fill in -- history list and mood chart on the
// left, a selected month's detail on the right, and the Start/Edit retro
// modal (Task 13, now wired in). Composition only, the GoalsPage.tsx/
// BillsPage.tsx convention: fetch orchestration lives in useRetros.ts, and
// no apiFetch call belongs here.
import { useState } from "react";
import { ApiError } from "../../api/client";
import { PageContainer } from "../../components/PageContainer";
import { MoodChart } from "./MoodChart";
import { RetroDetail } from "./RetroDetail";
import { RetroHistoryList } from "./RetroHistoryList";
import { RetroModal } from "./RetroModal";
import { RETRO_COPY, doneSinceClause, monthNameOnly } from "./retroCopy";
import { useRetros } from "./useRetros";

export function RetrosPage() {
  const retros = useRetros();
  // Owns the one write this page makes -- starting a retro. No per-row
  // tracking needed (GoalsPage.tsx's own pendingIds precedent covers many
  // rows at once; this page has exactly one startable month at a time), so
  // one flag and one error slot cover both Start buttons below (header and
  // first-run panel), which share this same handler.
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  // Which month's row is highlighted in the history list, and which month
  // RetroDetail (below) fetches and renders on the right.
  const [selectedMonth, setSelectedMonth] = useState<string | null>(null);
  // Which month's Start/Edit modal is open, if any -- a separate slot from
  // selectedMonth (GoalsPage.tsx's own modalGoal/contributingGoal split):
  // selecting a history row and editing it are two different actions that
  // can disagree about which month they're pointed at (Edit always targets
  // whatever is selected, but closing the modal must never clear the
  // selection underneath it).
  const [editingMonth, setEditingMonth] = useState<string | null>(null);

  // useRetros' own startRetro already invalidates both the list and the
  // created month's own detail query (retroQueryKeys.ts's header comment),
  // so the new draft appears in History below with no extra call this page
  // has to make. Opens straight into editing once the draft exists --
  // dc.html's own flow (clicking Start opens modalRetro immediately, not a
  // second click to find the new row in the list first) -- and selects the
  // same month in the history list so the detail panel behind the modal
  // already agrees with it once the modal closes.
  function handleStart() {
    setStarting(true);
    setStartError(null);
    retros
      .startRetro()
      .then((created) => {
        setSelectedMonth(created.month);
        setEditingMonth(created.month);
      })
      .catch((err: unknown) => {
        setStartError(err instanceof ApiError ? err.message : RETRO_COPY.startError);
      })
      .finally(() => setStarting(false));
  }

  if (retros.loading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }

  if (retros.error) {
    // GET /retros is marriage-AND-owner gated (retroCopy.ts's own comment on
    // why requireOwner is stacked even though a limited member can never
    // hold CapMarriage today) -- a household owner is the only caller this
    // route usually sees, but the guard is defence in depth, not dead code,
    // so this page still has to answer its own 403 honestly rather than
    // trusting that case can't happen. Branching on the real status, not a
    // second useMe() role check -- the same reasoning
    // GoalsPage.tsx/BillsPage.tsx/BudgetPage.tsx give on their identical
    // branch: a role check here would be a second source of truth that
    // could disagree with what the server actually decided.
    const status = retros.error instanceof ApiError ? retros.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="retros-owner-only" className="m-9 rounded-xl border border-hairline bg-card p-[22px]">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{RETRO_COPY.title}</h1>
          <h2 className="mt-4 text-xs text-muted">{RETRO_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{RETRO_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="retros-load-error" className="p-9 text-xs text-danger">
        {RETRO_COPY.loadError}
      </p>
    );
  }

  // retros.error is null and retros.loading is false here, so data is
  // present -- TanStack Query's own contract. This guards the type only, the
  // same defensive shape BudgetPage.tsx's `!budget.data` guard uses.
  if (!retros.data) {
    return null;
  }

  const data = retros.data;
  const subtitleClause = doneSinceClause(data);
  // Fails closed on the backend's own stated invariant
  // (retrosResponseSchema's comment: null means both candidate months
  // already have a retro) rather than trusting it blindly -- a button built
  // from a null month would throw inside monthNameOnly's own string split.
  const startLabel = data.startMonth ? RETRO_COPY.startRetro(monthNameOnly(data.startMonth)) : null;
  const noRetrosYet = data.retros.length === 0;

  return (
    <PageContainer data-testid="retros-page">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{RETRO_COPY.title}</h1>
          <p data-testid="retros-subtitle" className="mt-1 text-[13px] text-muted">
            {RETRO_COPY.subtitle}
            {subtitleClause && ` · ${subtitleClause}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {/* Not a button -- nothing on it opens anything (the design's own
              static badge). The 44px floor (CLAUDE.md) applies to
              interactive controls only, so this stays whatever height its
              own padding gives it. */}
          <span
            data-testid="retros-privacy-badge"
            className="inline-flex items-center rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] text-muted"
          >
            {RETRO_COPY.privacyBadge}
          </span>
          {startLabel && (
            <button
              type="button"
              data-testid="retros-start"
              onClick={handleStart}
              disabled={starting}
              // min-h-11/sm:min-h-0: TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured reason py-2 alone
              // falls short of the 44px floor on a phone.
              className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              {startLabel}
            </button>
          )}
        </div>
      </div>

      {startError && (
        <p role="alert" className="text-xs text-danger">
          {startError}
        </p>
      )}

      {noRetrosYet ? (
        <div data-testid="retros-empty-state" className="rounded-xl border border-hairline bg-card p-16 text-center">
          <div className="text-[19px] font-semibold tracking-[-0.01em] text-ink">{RETRO_COPY.emptyHeadline}</div>
          <p className="mx-auto mt-2 max-w-[420px] text-[13.5px] leading-relaxed text-muted">
            {RETRO_COPY.emptyBody}
          </p>
          {/* Distinct copy from the header's own startRetro() button above
              -- both render together the first time a household has zero
              retros and a startable month (BillsPage.tsx's own "+ Add
              bill"/"Create your first bill" precedent: identical text on two
              buttons on the same screen is two elements answering to one
              accessible name). Gated on startLabel, not unconditionally --
              retrosResponseSchema's own nullable startMonth means "nothing
              to start" is a real state this schema allows even though the
              service should never actually produce it alongside an empty
              history; this fails closed rather than rendering a button with
              nothing to click into. */}
          {startLabel && (
            <div className="mt-6 flex justify-center">
              <button
                type="button"
                data-testid="retros-create-first"
                onClick={handleStart}
                disabled={starting}
                className="min-h-11 rounded-lg bg-accent px-5 py-2.5 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
              >
                {RETRO_COPY.createFirstRetro}
              </button>
            </div>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-[340px_1fr]">
          <div data-testid="retro-history" className="rounded-xl border border-hairline bg-card p-[22px]">
            <h2 className="mb-4 text-sm font-semibold text-ink">{RETRO_COPY.historyTitle}</h2>
            <RetroHistoryList summaries={data.retros} onSelect={setSelectedMonth} selectedMonth={selectedMonth} />
            {/* data.mood is already fetched by useRetros.ts -- MoodChart
                renders straight off it, no second fetch (this mount point's
                own Task-10 comment, now made real). */}
            <div data-testid="retro-mood-chart-mount" className="mt-4 border-t border-hairline pt-4">
              <h3 className="mb-2 text-xs text-muted">{RETRO_COPY.moodChartTitle}</h3>
              <MoodChart points={data.mood} />
            </div>
          </div>

          {/* selectedMonth comes from RetroHistoryList's own onSelect
              (Task 11) -- nothing here fetches by month itself.
              RetroDetail.tsx owns its own useRetro(month) call and the tick;
              this mount point only decides whether a month is picked yet.
              Edit lives here rather than inside RetroDetail.tsx (which this
              task does not modify) -- it opens the same modal the Start
              button does, targeting whichever month is currently selected. */}
          <div data-testid="retro-detail-mount" className="rounded-xl border border-hairline bg-card p-6">
            {selectedMonth ? (
              <>
                <div className="mb-3 flex justify-end">
                  <button
                    type="button"
                    data-testid="retro-edit"
                    onClick={() => setEditingMonth(selectedMonth)}
                    className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[12.5px] font-semibold text-accent sm:min-h-0"
                  >
                    {RETRO_COPY.editRetro}
                  </button>
                </div>
                <RetroDetail month={selectedMonth} />
              </>
            ) : (
              // `h-full` resolves here because this div's parent is a grid item
              // stretched to the row the 340px history list sets, so its height
              // is definite even though nothing declares one -- checked in a
              // browser rather than assumed, since a percentage height against
              // an auto-height parent collapses to the content and centres
              // nothing while every class is still present.
              <div className="flex h-full flex-col items-center justify-center gap-1.5 px-8 text-center">
                <p className="text-[15px] text-ink">{RETRO_COPY.detailPlaceholder}</p>
                <p className="max-w-[42ch] text-[12.5px] leading-relaxed text-muted">
                  {RETRO_COPY.detailPlaceholderBody}
                </p>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Conditional mount, not a declarative `open` prop -- Modal.tsx's own
          header comment on why the two never mix. Renders for both entry
          points above: handleStart's own setEditingMonth once a draft
          exists, and the Edit button once a month is selected.

          `onDiscarded` clears `selectedMonth` too, not just `editingMonth` --
          a real browser walk against Discard draft found the gap this
          closes: without it, RetroDetail.tsx below stays pointed at the
          just-deleted month and renders "Couldn't load this retro." the
          instant the modal closes, right after a delete that actually
          succeeded. Checked against `editingMonth` (not cleared
          unconditionally) because `selectedMonth` and `editingMonth` are
          always the same month on every path that opens this modal
          (handleStart sets both together; Edit sets `editingMonth` FROM
          `selectedMonth`), so this is defensive rather than load-bearing --
          but a wrong assumption here would silently clear a selection
          Discard was never asked to touch. */}
      {editingMonth && (
        <RetroModal
          month={editingMonth}
          onClose={() => setEditingMonth(null)}
          onDiscarded={() => setSelectedMonth((current) => (current === editingMonth ? null : current))}
        />
      )}
    </PageContainer>
  );
}
