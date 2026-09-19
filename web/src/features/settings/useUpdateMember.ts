// Fetch orchestration for MembersPanel: PATCH /household/members/{id}.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { fetchAndParse } from "../../api/client";
import { meQueryKey } from "../auth/useAuth";
import { updateMemberResponseSchema } from "./schemas";
import { householdMembersQueryKey } from "./useHouseholdMembers";
import { spacesQueryKey } from "./useSpaces";

// api/internal/adapter/http/member_handlers.go's updateMemberRequest fields
// are now pointers (fixed alongside this task, matching /household and
// /notification-preferences): an absent field means "leave this alone,"
// resolved server-side against the membership's *current* role/capabilities
// before domain.ValidateMembershipChange ever runs. This mutation takes
// advantage of that -- role and capabilities are each optional, and only
// the one(s) actually changing are sent. A plain capability toggle
// (MembersPanel's toggleCapability) sends capabilities alone; a role change
// (its toggleRole) sends both, because promoting or demoting genuinely
// changes both fields at once, not because the endpoint demands it.
export function useUpdateMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      id: string;
      role?: string;
      capabilities?: string[];
    }) => {
      const patch: { role?: string; capabilities?: string[] } = {};
      if (vars.role !== undefined) patch.role = vars.role;
      if (vars.capabilities !== undefined) patch.capabilities = vars.capabilities;

      return fetchAndParse(
        updateMemberResponseSchema,
        `/api/v1/household/members/${encodeURIComponent(vars.id)}`,
        { method: "PATCH", body: JSON.stringify(patch) },
      );
    },
    // Returns the combined invalidation promise rather than firing all
    // three and letting onSuccess return undefined: TanStack Query awaits
    // whatever a mutation's onSuccess returns before treating the mutation
    // as settled, so onSettled (and therefore MembersPanel's pendingIds
    // cleanup, which is what re-enables a member's controls) only runs
    // once every invalidated query has actually refetched. Without the
    // `return`/`Promise.all`, invalidateQueries's returned promises would
    // be fire-and-forget: the PATCH response arriving would settle the
    // mutation immediately, re-enabling the row while ['household',
    // 'members'] was still serving its stale cached value -- the same
    // stale-array race the pendingIds guard exists to close, just moved
    // into the gap between "PATCH resolved" and "refetch landed" instead
    // of "click" and "PATCH resolved".
    onSuccess: () => {
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: householdMembersQueryKey }),
        // The sidebar and every RequireCapability guard read from ['me'];
        // a capability or role change that doesn't refresh it leaves the
        // caller (if they just edited their own membership) looking at
        // stale navigation.
        queryClient.invalidateQueries({ queryKey: meQueryKey }),
        // SpacesPanel reads its own, separately keyed ['spaces'] query.
        // domain.VisibleSpaces filters by role/capabilities, so a role or
        // capability change here can change which spaces the caller (if
        // they just edited their own membership -- e.g. an owner demoting
        // themselves) is allowed to see. Without this, SpacesPanel would
        // keep listing a space like Marriage as visible after its own
        // viewer lost the access that used to grant it.
        queryClient.invalidateQueries({ queryKey: spacesQueryKey }),
      ]);
    },
  });
}
