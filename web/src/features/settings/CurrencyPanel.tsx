// The Currency & region panel (design/Household Dashboard.dc.html's
// Settings screen, third card). Only "Show {X} equivalents" is interactive
// in the design's own mockup -- the primary-currency row and the FX-rate row
// carry no onClick at all, so both render as plain text here too.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { householdSchema, type Household } from "../auth/schemas";
import { useMe } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { currencyLabel } from "./copy";

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
    mutationFn: async (vars: { showSecondaryCurrency: boolean }): Promise<Household> => {
      const body = await apiFetch<unknown>("/api/v1/household", {
        method: "PATCH",
        body: JSON.stringify(vars),
      });
      return householdSchema.parse(body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["household"] });
      queryClient.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

export function CurrencyPanel() {
  const me = useMe();
  const household = useHousehold();
  const updateHousehold = useUpdateHousehold();
  const isOwner = me.data?.membership.role === "owner";

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
            <span className="text-ink">Primary currency</span>
            <span className="rounded-lg border border-hairline px-3 py-1.5 font-semibold text-ink">
              {currencyLabel(household.data.primaryCurrency)}
            </span>
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
              Something went wrong saving that. Please try again.
            </p>
          )}
        </div>
      )}
    </section>
  );
}
