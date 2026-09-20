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
import { meQueryKey } from "../auth/useAuth";
import {
  admitResultSchema,
  inviteLinkSchema,
  pendingInvitesSchema,
  type AdmitResult,
  type InviteLink,
  type PendingInvite,
} from "./schemas";
import { householdMembersQueryKey } from "./useHouseholdMembers";

export const pendingInvitesQueryKey = ["household", "invites"] as const;

async function fetchPendingInvites(): Promise<PendingInvite[]> {
  return fetchAndParse(pendingInvitesSchema, "/api/v1/household/invites");
}

// invitePollInterval is exported so a test can assert the rule directly:
// proving it over real elapsed time would mean waiting out several
// three-second polls (the reason TelegramPanel.test.tsx gives for the same
// shape, over telegramPollInterval).
//
// The poll exists for one event -- a knock arriving from a phone the
// browser cannot hear about any other way. It runs only while something
// could still knock, and stops the moment one has, so a settled Settings
// tab is not refetching every three seconds forever.
export function invitePollInterval(invites: PendingInvite[] | undefined): number | false {
  const waiting = (invites ?? []).some(
    (invite) => invite.channel === "telegram" && !invite.knock,
  );
  return waiting ? 3000 : false;
}

export function usePendingInvites({ enabled }: { enabled: boolean }) {
  return useQuery({
    queryKey: pendingInvitesQueryKey,
    queryFn: fetchPendingInvites,
    enabled,
    refetchInterval: (query) => invitePollInterval(query.state.data),
    // A hidden tab has nobody watching it. TanStack's default already
    // pauses interval refetching in the background; this states it, because
    // the whole point of the interval is a person looking at the screen.
    refetchIntervalInBackground: false,
  });
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

// useNewInviteLink mints a fresh one-time link for a Telegram invite --
// "Get a new link" on an expired link, and "Not them" on a wrong knock
// (invite_lobby_handlers.go's handleNewInviteLink serves both with the same
// effect).
export function useNewInviteLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<InviteLink> =>
      fetchAndParse(inviteLinkSchema, `/api/v1/household/invites/${encodeURIComponent(id)}/link`, {
        method: "POST",
      }),
    // The knock is cleared server-side, so the row on screen is stale the
    // moment this returns -- and it is stale on a failure too (another
    // owner may have withdrawn the invite), which is why this is onSettled.
    onSettled: () => queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
  });
}

// useAdmitInvite turns a knock into a member. The request carries no body:
// the four digits are compared by eye against the phone in front of the
// owner, and no endpoint accepts them back (spec decision 3).
export function useAdmitInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<AdmitResult> =>
      fetchAndParse(admitResultSchema, `/api/v1/household/invites/${encodeURIComponent(id)}/admit`, {
        method: "POST",
      }),
    // Three lists change, and naming each one is the rule
    // docs/LEARNING.md pattern 22 exists for: the invite leaves the pending
    // list, the member joins the members list, and Overview's setup
    // checklist counts owners -- a screen that reads a derived figure needs
    // its query invalidated by name, or it shows yesterday's answer.
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
        queryClient.invalidateQueries({ queryKey: householdMembersQueryKey }),
        queryClient.invalidateQueries({ queryKey: meQueryKey }),
      ]),
  });
}
