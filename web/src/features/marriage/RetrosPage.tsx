// The Retros screen: header (title, subtitle, done-count clause, privacy
// badge, start button), the five states from the task-10 brief, and the
// mount points Tasks 11-13 fill in -- history list and mood chart on the
// left, a selected month's detail on the right, and the Start/Edit retro
// modal (Task 13) opening from this same handler once it exists. Composition
// only, the GoalsPage.tsx/BillsPage.tsx convention: fetch orchestration
// lives in useRetros.ts, and no apiFetch call belongs here.
import { useState } from "react";
import { ApiError } from "../../api/client";
import { PageContainer } from "../../components/PageContainer";
import { RETRO_COPY, doneSinceClause, monthNameOnly, monthYearLabel } from "./retroCopy";
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

  // useRetros' own startRetro already invalidates both the list and the
  // created month's own detail query (retroQueryKeys.ts's header comment),
  // so the new draft appears in History below with no extra call this page
  // has to make. Task 13's modal will eventually open from this same
  // handler once a draft exists to edit; today it only creates the draft.
  function handleStart() {
    setStarting(true);
    setStartError(null);
    retros
      .startRetro()
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
          <span className="inline-flex items-center rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] text-muted">
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
            {/* Task 11 replaces this list with the real RetroHistoryList --
                the draft's own row (below) is real behaviour this task must
                ship (state 2 of the brief); everything else here is the
                minimal stand-in for that later component, not a finished
                design. Server order preserved, never re-sorted or filtered
                (Sidebar.tsx's own rule, for the identical reason: the server
                has already ordered these). Neither row is clickable yet --
                Task 12 wires history selection into the detail mount below;
                a non-interactive row needs no 44px floor (CLAUDE.md's floor
                applies to interactive controls). */}
            <div className="flex flex-col gap-1">
              {data.retros.map((summary) =>
                summary.finished ? (
                  <div
                    key={summary.id}
                    data-testid="retro-summary-row"
                    className="rounded-lg px-3 py-2 text-[13.5px]"
                  >
                    <div className="font-semibold text-ink">{monthYearLabel(summary.month)}</div>
                    <div className="mt-0.5 text-[11.5px] text-muted">
                      {[
                        summary.mood !== null ? `Mood ${summary.mood}/5` : null,
                        `${summary.actionCount} action${summary.actionCount === 1 ? "" : "s"}`,
                        summary.quote ? `"${summary.quote}"` : null,
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </div>
                  </div>
                ) : (
                  <div
                    key={summary.id}
                    data-testid="retro-draft-row"
                    className="rounded-lg bg-callout px-3 py-2 text-[13.5px]"
                  >
                    <div className="font-semibold text-ink">{monthYearLabel(summary.month)}</div>
                    <div className="mt-0.5 text-[11.5px] font-semibold text-accent">
                      {RETRO_COPY.draftInProgress}
                    </div>
                  </div>
                ),
              )}
            </div>
            {/* Task 11's own mount point for the twelve-month mood chart --
                data.mood is already fetched by useRetros.ts and sits unused
                here on purpose, so that task wires the real chart against
                data this page already has rather than adding a second
                fetch. */}
            <div data-testid="retro-mood-chart-mount" className="mt-4 border-t border-hairline pt-4">
              <h3 className="text-xs text-muted">{RETRO_COPY.moodChartTitle}</h3>
            </div>
          </div>

          {/* Task 12's own mount point for a selected retro's detail (went
              well / was hard / actions / notes) -- needs useRetro(month) for
              a specific month, which this page does not call; picking which
              month is Task 11's own history-list interaction, so there is
              nothing to select from yet. */}
          <div data-testid="retro-detail-mount" className="rounded-xl border border-hairline bg-card p-6">
            <p className="text-[13px] text-muted">{RETRO_COPY.detailPlaceholder}</p>
          </div>
        </div>
      )}
    </PageContainer>
  );
}
