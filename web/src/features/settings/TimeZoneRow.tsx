// The "Time zone" row of Settings' Currency & region card.
//
// A household keeps one calendar: this zone decides which day "today" is for
// every member, on every screen and in the bot, whatever zone each of them
// is in. Changing it rewrites no stored date. It changes which day counts as
// today from the next request on.
import { type FormEvent, useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { TimeZoneSelect } from "../../components/TimeZoneSelect";
import { longDateIn } from "../../lib/householdDate";
import { useMe } from "../auth/useAuth";
import { useHousehold, useUpdateHousehold } from "./useHousehold";

export function TimeZoneRow() {
  const me = useMe();
  const household = useHousehold();
  // Its own mutation, not CurrencyPanel's: a refused zone must not show up
  // as an error under the currency field, nor a pending currency save
  // disable this select.
  const updateHousehold = useUpdateHousehold();
  const isOwner = me.data?.membership.role === "owner";

  // null until the owner picks a zone; until then the select shows what is
  // stored, including after a refetch brings a new value. Once they pick,
  // their choice stays on screen even if the save is refused, so they can
  // correct it rather than find it silently put back.
  const [chosen, setChosen] = useState<string | null>(null);

  if (!household.isSuccess) return null;

  const stored = household.data.timezone;
  const zone = chosen ?? stored;
  const canSave = isOwner && zone !== "" && zone !== stored && !updateHousehold.isPending;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSave) return;
    updateHousehold.mutate({ timezone: zone }, { onSuccess: () => setChosen(null) });
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        {/* <label htmlFor> only when the select it names renders, the same
            rule CurrencyPanel's primary-currency row follows: a limited
            member must not get a label pointing at a control that is not
            in the page. */}
        {isOwner ? (
          <label htmlFor="household-time-zone" className="text-ink">
            Time zone
          </label>
        ) : (
          <span className="text-ink">Time zone</span>
        )}
        {isOwner ? (
          <form onSubmit={handleSubmit} className="flex min-w-0 items-center gap-2">
            <TimeZoneSelect
              id="household-time-zone"
              value={zone}
              onChange={setChosen}
              disabled={updateHousehold.isPending}
              // max-w: the longest label ("America/Argentina/ComodRivadavia
              // (GMT-3)") must not push the card wider than a phone.
              className="min-h-11 min-w-0 max-w-[14rem] rounded-lg border border-hairline bg-card px-2 py-2.5 text-ink disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
            />
            <button
              type="submit"
              disabled={!canSave}
              aria-label="Save time zone"
              className="min-h-11 rounded-lg bg-accent px-2.5 py-2.5 text-[11px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
            >
              Save
            </button>
          </form>
        ) : (
          <span className="rounded-lg border border-hairline px-3 py-1.5 font-semibold text-ink">
            {stored.replaceAll("_", " ")}
          </span>
        )}
      </div>
      {/* The date in the zone on screen, saved or not: the one consequence of
          this control a person can check against their own calendar before
          they commit to it. */}
      <div className="text-[11.5px] text-muted">
        Decides which day is “today” for everyone in the household. There, today is{" "}
        <span data-testid="time-zone-today">{longDateIn(zone)}</span>.
      </div>
      {updateHousehold.isError && (
        <p role="alert" className="text-[11px] text-danger">
          {apiErrorMessage(updateHousehold.error, "Something went wrong saving that. Please try again.")}
        </p>
      )}
    </div>
  );
}
