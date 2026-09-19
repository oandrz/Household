// The household's pending invites -- sent, not accepted, not expired -- read
// by Settings' Members panel and Overview's setup checklist. The route is
// owner-only, and a limited member must never fire a request whose 403 would
// then need hiding. Overview passes `enabled: isOwner`. PendingInvitesList
// passes `true`, because MembersPanel mounts it only for an owner.
//
// The key starts with "household" for the reason householdMembersQueryKey's
// does (useHouseholdMembers.ts): PATCH /household invalidates by that prefix.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch, fetchAndParse } from "../../api/client";
import { pendingInvitesSchema, type PendingInvite } from "./schemas";
import { householdMembersQueryKey } from "./useHouseholdMembers";

export const pendingInvitesQueryKey = ["household", "invites"] as const;

async function fetchPendingInvites(): Promise<PendingInvite[]> {
  return fetchAndParse(pendingInvitesSchema, "/api/v1/household/invites");
}

export function usePendingInvites({ enabled }: { enabled: boolean }) {
  return useQuery({ queryKey: pendingInvitesQueryKey, queryFn: fetchPendingInvites, enabled });
}

export function useWithdrawInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    // 204, so nothing to parse: the mutation's only job is the DELETE.
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/household/invites/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
    },
    // onSettled, not onSuccess: a failed withdraw is news too. A 404 means
    // another owner already withdrew the invite, and a 409 means it was
    // accepted meanwhile. Either way the row on screen is stale, and only a
    // refetch removes it. A 409 also means the invitee is a member now, so
    // the members list is refetched as well. The promise is returned so the
    // mutation stays pending -- and Withdraw stays disabled -- until the
    // refetch has landed.
    onSettled: (_data, error) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
        error instanceof ApiError && error.status === 409
          ? queryClient.invalidateQueries({ queryKey: householdMembersQueryKey })
          : undefined,
      ]),
  });
}
