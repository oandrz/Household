// The household's pending invites -- sent, not accepted, not expired -- read
// by Settings' Members panel and Overview's setup checklist. The route is
// owner-only, so callers pass `enabled: isOwner` and a limited member never
// fires a request whose 403 would then need hiding.
//
// The key starts with "household" for the reason householdMembersQueryKey's
// does (useHouseholdMembers.ts): PATCH /household invalidates by that prefix.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, fetchAndParse } from "../../api/client";
import { pendingInvitesSchema, type PendingInvite } from "./schemas";

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
    onSuccess: () => queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
  });
}
