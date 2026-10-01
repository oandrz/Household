// The carry-over offer inside the retro modal (design decision 4): last
// month's still-open actions, one row each, offered only while there is
// something left to offer -- an empty list renders nothing at all, not an
// empty heading over a blank list (the same "no placeholder for an absence"
// rule the rest of the retro screen already follows). Pulled out of
// RetroModal.tsx; its state, handler and comments moved here unchanged.
import { useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { RETRO_COPY, previousMonthName } from "./retroCopy";
import type { RetroAction } from "./retroSchemas";
import type { useRetro } from "./useRetro";

export function CarryOverList({
  month,
  carryOver,
  addAction,
  disabled,
}: {
  month: string;
  // Always `retro.data.carryOver`, never a list built anywhere else -- see
  // handleCarryOver's own comment for the trust boundary that depends on it.
  carryOver: RetroAction[];
  addAction: ReturnType<typeof useRetro>["addAction"];
  // useRetroDraft's `actionsDisabled`: a save in flight, or the conflict latch.
  disabled: boolean;
}) {
  // Which of `carryOver`'s own action ids this mount has already posted a
  // carry for -- purely a local, per-mount safeguard against a double click,
  // NOT something the server tracks. Confirmed by reading the query behind
  // OpenInMonth (retro.sql's own ListOpenActionsInMonth): it excludes an
  // action only once ITS OWN done_at is set, and carrying one never touches
  // July's own row (design decision 4: "July's own row is untouched and
  // stays unticked") -- there is no server-side de-dup, and no unique
  // constraint on carried_from either (migrations/00009_retros.sql), so the
  // same July action would be offered again forever without this. A fresh
  // mount (closing and reopening the modal) starts this set empty again,
  // same as `hadConflict`'s own "fresh mount, fresh everything" contract --
  // the offer reappearing after a reload is the accepted, honest behaviour,
  // not a bug this is trying to hide.
  const [carriedIds, setCarriedIds] = useState<Set<string>>(new Set());
  const [carryingId, setCarryingId] = useState<string | null>(null);
  const [carryError, setCarryError] = useState<string | null>(null);

  // Carries one of last month's still-open actions onto THIS retro. `action`
  // is always an element of `retro.data.carryOver` -- never a freehand id --
  // because the only caller is the map below, which only ever hands this
  // function an entry from that same list. useRetro.ts's own
  // AddRetroActionBody doc comment names this exact trust boundary: passing
  // anything else would silently break RetroActionRow's "Carried from
  // {month}" label, which infers the source month by calendar arithmetic
  // rather than reading it off the wire (retroActionDTO carries no month for
  // a carried-from id at all).
  //
  // Checked against `disabled` up front, the same explicit-invariant reason
  // useRetroDraft's `finish` checks it rather than leaning on the button's own
  // `disabled` attribute alone -- once `hadConflict` latches, nothing in the
  // modal should still be writing against a retro this tab's own
  // understanding of may already be stale.
  async function handleCarryOver(action: RetroAction) {
    if (disabled) return;
    setCarryError(null);
    setCarryingId(action.id);
    try {
      await addAction({ body: action.body, carriedFrom: action.id });
      setCarriedIds((prev) => new Set(prev).add(action.id));
    } catch (err) {
      setCarryError(apiErrorMessage(err, RETRO_COPY.carryOverError));
    } finally {
      setCarryingId(null);
    }
  }

  // Filtered by `carriedIds` BEFORE the "is there anything to show" check
  // below -- gating that check on the server's own unfiltered
  // `carryOver.length` instead would leave the "Still open from July" heading
  // on screen with an empty list under it once every row in this mount has
  // been carried, the exact "heading with nothing under it" shape the retro
  // screen's own BulletCard/actions-list guards elsewhere all refuse to render.
  const openCarryOver = carryOver.filter((action) => !carriedIds.has(action.id));
  if (openCarryOver.length === 0) return null;

  return (
    <>
      <h3 className="text-xs font-semibold text-label">
        {RETRO_COPY.carryOverHeading(previousMonthName(month))}
      </h3>
      <div className="flex flex-col gap-2">
        {openCarryOver.map((action) => (
          <div
            key={action.id}
            className="flex items-center justify-between gap-3 rounded-[10px] border border-hairline px-3.5 py-2.5"
          >
            <span className="text-[13px] text-ink">{action.body}</span>
            <button
              type="button"
              aria-label={RETRO_COPY.carryOverButton(action.body)}
              disabled={disabled || carryingId === action.id}
              onClick={() => void handleCarryOver(action)}
              className="min-h-11 flex-none rounded-lg border border-hairline px-3 text-[12px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
            >
              {RETRO_COPY.carryOverButtonLabel}
            </button>
          </div>
        ))}
      </div>
      {carryError && (
        <p role="alert" className="text-xs leading-snug text-danger">
          {carryError}
        </p>
      )}
    </>
  );
}
