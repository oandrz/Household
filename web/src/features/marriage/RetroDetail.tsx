// The right-hand panel of the Retros screen once a month is selected --
// went well, was hard, the actions and their assignees, and notes
// (dc.html's own "June 2026 retro" card). This is also the one place that
// owns the tick: RetroActionRow is presentational, every write goes through
// useRetro(month)'s own setActionDone, which sends `{ done }` alone and
// never the retro's own `version` (useRetro.ts's own header comment on why
// -- a tick writes a different table precisely so one partner ticking all
// month cannot collide with the other's open editor).
//
// Mounted only once RetrosPage.tsx has a selectedMonth (its own mount-point
// comment) -- month is never undefined here.
import { useState } from "react";
import { ApiError } from "../../api/client";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { RetroActionRow } from "./RetroActionRow";
import { RETRO_COPY, completedDateLabel, monthYearLabel, nextMonthName } from "./retroCopy";
import { useRetro } from "./useRetro";

// Splits a went-well/was-hard textarea into the design's own one-bullet-per-
// line rendering (dc.html: each clause is one "· ..." line). Blank lines are
// dropped rather than rendered as an empty bullet -- a household that
// pressed Enter twice while typing should not see a stray bullet with
// nothing after it.
function splitBullets(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function BulletCard({
  testId,
  heading,
  headingClassName,
  cardClassName,
  lines,
}: {
  testId: string;
  heading: string;
  headingClassName: string;
  cardClassName: string;
  lines: string[];
}) {
  // Guarded on having something to show, the same "no placeholder for an
  // absence" rule this screen already follows elsewhere (the history row's
  // quote/action-count clauses) -- an empty box with a heading and nothing
  // under it reads as broken, not as "nothing written yet".
  if (lines.length === 0) return null;
  return (
    <div className={cardClassName}>
      <div className={`mb-2.5 text-xs font-semibold ${headingClassName}`}>{heading}</div>
      <ul data-testid={testId} className="flex list-disc flex-col gap-2 pl-4 text-[13px] leading-relaxed text-ink">
        {lines.map((line, index) => (
          // Index keys are safe here: this list is a pure re-render of a
          // freshly split string on every render, never reordered or
          // individually added to/removed from by the reader.
          <li key={index}>{line}</li>
        ))}
      </ul>
    </div>
  );
}

export function RetroDetail({ month }: { month: string }) {
  const retro = useRetro(month);
  const members = useHouseholdMembers();
  // GoalsPage.tsx's own pendingIds reasoning: useRetro exposes one
  // setActionDone shared by every row, so "is a tick for this id in flight"
  // has to live here, not inside a single shared mutation's own isPending.
  const [pendingActionIds, setPendingActionIds] = useState<Set<string>>(new Set());
  const [tickError, setTickError] = useState<string | null>(null);

  function handleToggle(actionId: string, done: boolean) {
    setTickError(null);
    setPendingActionIds((prev) => new Set(prev).add(actionId));
    retro
      .setActionDone(actionId, done)
      .catch((err: unknown) => {
        setTickError(err instanceof ApiError ? err.message : RETRO_COPY.tickError);
      })
      .finally(() => {
        setPendingActionIds((prev) => {
          const next = new Set(prev);
          next.delete(actionId);
          return next;
        });
      });
  }

  if (retro.loading) {
    return <p className="text-xs text-muted">Loading…</p>;
  }

  if (retro.error) {
    return (
      <p role="alert" data-testid="retro-detail-load-error" className="text-xs text-danger">
        {RETRO_COPY.detailLoadError}
      </p>
    );
  }

  // retro.error is null and retro.loading is false here, so data is present
  // -- TanStack Query's own contract, the same defensive-only guard
  // RetrosPage.tsx's `!retros.data` check uses.
  if (!retro.data) {
    return null;
  }

  const record = retro.data.retro;
  const wentWellLines = splitBullets(record.wentWell);
  const wasHardLines = splitBullets(record.wasHard);
  const notesLines = record.notes.trim();

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 data-testid="retro-detail-heading" className="text-[16px] font-semibold tracking-[-0.01em] text-ink">
          {monthYearLabel(record.month)} retro
        </h2>
        <p className="text-xs text-muted">
          {[record.completedAt ? completedDateLabel(record.completedAt) : null, record.mood !== null ? RETRO_COPY.moodLabel(record.mood) : null]
            .filter(Boolean)
            .join(" · ")}
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <BulletCard
          testId="retro-went-well"
          heading={RETRO_COPY.wentWellHeading}
          headingClassName="text-accent"
          cardClassName="rounded-[10px] bg-callout p-4"
          lines={wentWellLines}
        />
        <BulletCard
          testId="retro-was-hard"
          heading={RETRO_COPY.wasHardHeading}
          headingClassName="text-danger"
          cardClassName="rounded-[10px] bg-danger-soft p-4"
          lines={wasHardLines}
        />
      </div>

      {record.actions.length > 0 && (
        <div>
          <div className="mb-2.5 text-xs font-semibold text-muted">{RETRO_COPY.actionsHeading(nextMonthName(record.month))}</div>
          <div className="flex flex-col gap-2.5">
            {record.actions.map((action) => (
              <RetroActionRow
                key={action.id}
                action={action}
                members={members.data ?? []}
                retroMonth={record.month}
                pending={pendingActionIds.has(action.id)}
                onToggle={handleToggle}
              />
            ))}
          </div>
          {tickError && (
            <p role="alert" className="mt-2 text-xs text-danger">
              {tickError}
            </p>
          )}
        </div>
      )}

      {notesLines !== "" && (
        <div className="rounded-[10px] border border-hairline bg-canvas p-3.5">
          <div className="mb-1.5 text-xs font-semibold text-muted">{RETRO_COPY.notesHeading}</div>
          <p className="text-[13px] leading-relaxed text-ink">{record.notes}</p>
        </div>
      )}
    </div>
  );
}
