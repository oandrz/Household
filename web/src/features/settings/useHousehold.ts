// Fetch orchestration for CurrencyPanel: GET and PATCH /household.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchAndParse } from "../../api/client";
import { householdSchema, type Household } from "../auth/schemas";
import { meQueryKey } from "../auth/useAuth";

// TanStack Query matches invalidations by prefix, so invalidating this key
// also refreshes householdMembersQueryKey (["household", "members"]).
const householdQueryKey = ["household"] as const;

async function fetchHousehold(): Promise<Household> {
  return fetchAndParse(householdSchema, "/api/v1/household");
}

export function useHousehold() {
  return useQuery({ queryKey: householdQueryKey, queryFn: fetchHousehold });
}

export function useUpdateHousehold() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      vars: { showSecondaryCurrency: boolean } | { primaryCurrency: string },
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
