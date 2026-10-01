// Fetch orchestration for CurrencyPanel and TimeZoneRow: GET and PATCH
// /household.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { fetchAndParse } from "../../api/client";
import { householdSchema, type Household } from "../auth/schemas";
import { meQueryKey } from "../auth/useAuth";

// GET /household's body: the household, plus what Settings must know before it
// offers a field. primaryCurrencyLocked is true when the server would refuse a
// change of primary currency, which is once the household holds investments.
//
// It is required, with no default. Don't default it to false: "not locked" is
// the wrong answer to assume, and it is what the screen showed before the
// server said otherwise. A server that does not send it fails this one card,
// not sign-in, because the household inside GET /auth/me has its own schema.
const householdSettingsSchema = householdSchema.extend({
  primaryCurrencyLocked: z.boolean(),
});
export type HouseholdSettings = z.infer<typeof householdSettingsSchema>;

// TanStack Query matches invalidations by prefix, so invalidating this key
// also refreshes householdMembersQueryKey (["household", "members"]).
//
// Exported for the one write outside Settings that changes what GET /household
// says: adding a holding, which locks the primary currency (useHoldings.ts).
export const householdQueryKey = ["household"] as const;

async function fetchHousehold(): Promise<HouseholdSettings> {
  return fetchAndParse(householdSettingsSchema, "/api/v1/household");
}

export function useHousehold() {
  return useQuery({ queryKey: householdQueryKey, queryFn: fetchHousehold });
}

export function useUpdateHousehold() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      vars: { showSecondaryCurrency: boolean } | { primaryCurrency: string } | { timezone: string },
    ): Promise<Household> => {
      return fetchAndParse(householdSchema, "/api/v1/household", {
        method: "PATCH",
        body: JSON.stringify(vars),
      });
    },
    // Returns (rather than fires-and-forgets) the invalidation promises: a
    // mutation's onSuccess return value is awaited by TanStack Query before
    // the mutation is considered settled, which is what `isPending` (the
    // toggle's disabled condition in CurrencyPanel) reflects. Without this, the
    // PATCH response arriving would immediately re-enable the toggle while
    // ['household'] was still serving its stale cached value -- a second
    // click in that gap would compute `!household.data.showSecondaryCurrency`
    // from the same pre-click value the first click already read.
    onSuccess: () => {
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: householdQueryKey }),
        queryClient.invalidateQueries({ queryKey: meQueryKey }),
      ]);
    },
  });
}
