// The Currency & region panel (design/Household Dashboard.dc.html's
// Settings screen, third card). The design's own mockup only makes "Show
// {X} equivalents" interactive -- the FX-rate row still carries no onClick
// and renders as plain text, matching that. The primary-currency row used
// to match it too, but the backend has always fully supported changing it
// (PATCH /household's primaryCurrency field, requireOwner-gated, validated
// through domain.NewMoney) -- this panel was just the only place with no
// way to reach it. It is now the one owner-only control in this panel
// without a mockup precedent, added because of that gap rather than
// because the design asked for it.
import { type FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { householdSchema, type Household } from "../auth/schemas";
import { useMe } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { currencyLabel } from "./copy";

// Mirrors the backend's own rule (api/internal/domain/money.go's NewMoney:
// "currency must be three letters" and "must be uppercase") so an obviously
// invalid code never leaves the browser -- the 422 INVALID_CURRENCY
// response is still what actually gates anything the backend itself would
// reject (an uppercase three-letter code that isn't a real currency, say);
// this is a client-side head start, not a replacement for it.
const CURRENCY_CODE_PATTERN = /^[A-Z]{3}$/;

async function fetchHousehold(): Promise<Household> {
  const body = await apiFetch<unknown>("/api/v1/household");
  return householdSchema.parse(body);
}

function useHousehold() {
  return useQuery({ queryKey: ["household"], queryFn: fetchHousehold });
}

function useUpdateHousehold() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      vars: { showSecondaryCurrency: boolean } | { primaryCurrency: string },
    ): Promise<Household> => {
      const body = await apiFetch<unknown>("/api/v1/household", {
        method: "PATCH",
        body: JSON.stringify(vars),
      });
      return householdSchema.parse(body);
    },
    // Returns (rather than fires-and-forgets) the invalidation promises: a
    // mutation's onSuccess return value is awaited by TanStack Query before
    // the mutation is considered settled, which is what `isPending` (the
    // toggle's disabled condition below) reflects. Without this, the PATCH
    // response arriving would immediately re-enable the toggle while
    // ['household'] was still serving its stale cached value -- a second
    // click in that gap would compute `!household.data.showSecondaryCurrency`
    // from the same pre-click value the first click already read.
    onSuccess: () => {
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: ["household"] }),
        queryClient.invalidateQueries({ queryKey: ["me"] }),
      ]);
    },
  });
}

export function CurrencyPanel() {
  const me = useMe();
  const household = useHousehold();
  const updateHousehold = useUpdateHousehold();
  const isOwner = me.data?.membership.role === "owner";

  // currencyInput mirrors household.data.primaryCurrency until the owner
  // starts typing (currencyTouched) -- the "derive state from props during
  // render, stop once the user takes over" pattern React's own docs
  // describe for syncing a controlled input to server data without a
  // separate effect. Untouched, it tracks the fetched value, including
  // across a refetch from something else changing it. Touched, it holds
  // exactly what the owner typed, so a rejected save (422) leaves their
  // attempted value on screen to correct rather than silently reverting it.
  const [currencyInput, setCurrencyInput] = useState("");
  const [currencyTouched, setCurrencyTouched] = useState(false);
  if (household.isSuccess && !currencyTouched && currencyInput !== household.data.primaryCurrency) {
    setCurrencyInput(household.data.primaryCurrency);
  }

  const trimmedCurrencyInput = currencyInput.trim().toUpperCase();
  const canSaveCurrency =
    isOwner &&
    household.isSuccess &&
    CURRENCY_CODE_PATTERN.test(trimmedCurrencyInput) &&
    trimmedCurrencyInput !== household.data.primaryCurrency &&
    !updateHousehold.isPending;

  function handleCurrencySubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSaveCurrency) return;
    // Triggered by a form submit (a click or an Enter keypress), not a
    // mount effect -- unlike MagicLinkConsumeScreen's old bug (see
    // useAuth.ts's useConsumeMagicLink comment), a per-call mutate()
    // onSuccess fired from here is subscribed for the component's whole
    // lifetime up to this point, not torn down and rebuilt the way a mount
    // effect's subscription is, so it's safe to rely on: the same pattern
    // AcceptInviteForm and NewSpaceModal already use for a form submit.
    updateHousehold.mutate(
      { primaryCurrency: trimmedCurrencyInput },
      { onSuccess: () => setCurrencyTouched(false) },
    );
  }

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 className="mb-4 text-sm font-semibold text-ink">Currency &amp; region</h2>

      {household.isPending && <p className="text-xs text-muted">Loading…</p>}
      {household.isError && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load the household's currency settings.
        </p>
      )}

      {household.isSuccess && (
        <div className="flex flex-col gap-3.5 text-[13px]">
          <div className="flex items-center justify-between">
            <label htmlFor="primary-currency" className="text-ink">
              Primary currency
            </label>
            {isOwner ? (
              <form onSubmit={handleCurrencySubmit} className="flex items-center gap-2">
                <input
                  id="primary-currency"
                  type="text"
                  value={currencyInput}
                  disabled={updateHousehold.isPending}
                  onChange={(event) => {
                    setCurrencyTouched(true);
                    setCurrencyInput(event.target.value.toUpperCase().slice(0, 3));
                  }}
                  maxLength={3}
                  className="w-16 rounded-lg border border-hairline px-3 py-1.5 text-center font-semibold uppercase text-ink disabled:cursor-not-allowed disabled:opacity-60"
                />
                <button
                  type="submit"
                  disabled={!canSaveCurrency}
                  className="rounded-lg bg-accent px-2.5 py-1.5 text-[11px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
                >
                  Save
                </button>
              </form>
            ) : (
              <span className="rounded-lg border border-hairline px-3 py-1.5 font-semibold text-ink">
                {currencyLabel(household.data.primaryCurrency)}
              </span>
            )}
          </div>

          <div className="flex items-center justify-between">
            <div>
              <div className="text-ink">
                Show {household.data.secondaryCurrency} equivalents
              </div>
              {/* Literal design copy, true of this specific seeded
                  household (Christine's accounts are in Indonesia) -- not
                  derived from any field, the same way Sidebar.tsx's "Andreas
                  & Christine" footer isn't a coincidence either. A household
                  with a different story would need this generalised or
                  made configurable; flagged in the report, not solved
                  here. */}
              <div className="mt-0.5 text-[11.5px] text-muted">
                For Christine's Indonesian accounts
              </div>
            </div>
            <ToggleSwitch
              checked={household.data.showSecondaryCurrency}
              disabled={!isOwner || updateHousehold.isPending}
              onChange={() =>
                updateHousehold.mutate({
                  showSecondaryCurrency: !household.data.showSecondaryCurrency,
                })
              }
              label="Show secondary currency equivalents"
            />
          </div>

          <div className="flex items-center justify-between">
            <span className="text-ink">FX rate</span>
            {/* domain.Household.FXRateMode is "inert until a live provider
                exists" (api/internal/domain/identity.go) -- there is no live
                numeric rate to show yet, unlike the design's "S$1 = Rp
                12,410 · auto". Rendering only the mode itself is honest
                about what this slice actually has; a live rate is a later
                slice's job, not something to fabricate here. */}
            <span className="text-muted">
              {household.data.fxRateMode === "auto" ? "Auto" : "Manual"}
            </span>
          </div>

          {updateHousehold.isError && (
            <p role="alert" className="text-[11px] text-danger">
              {apiErrorMessage(updateHousehold.error, "Something went wrong saving that. Please try again.")}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
