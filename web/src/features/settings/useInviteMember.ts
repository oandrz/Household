// Fetch orchestration for InviteMemberModal: POST /household/members/invite.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { fetchAndParse } from "../../api/client";
import { meQueryKey } from "../auth/useAuth";
import { inviteCreatedSchema, type InviteCreated } from "./schemas";
import { householdMembersQueryKey } from "./useHouseholdMembers";
import { pendingInvitesQueryKey } from "./usePendingInvites";

export type RoleOption = "owner" | "limited";

export const ROLE_OPTIONS: readonly RoleOption[] = ["owner", "limited"];

// How the invited person will sign in -- sent to the server as `channel`,
// and required (member_handlers.go's parseInviteChannelChoice refuses an
// omitted one). "profile" has one more value than domain.InviteChannel on
// the server: it means "this person never signs in at all," so it writes no
// invite row. Kept as its own type here rather than folded into RoleOption:
// a limited member can be invited on either "profile" or "telegram" -- the
// two are independent choices on the wire, even though the UI only ever
// offers "email" to an owner.
export type InviteChannel = "profile" | "email" | "telegram";

async function inviteMember(vars: {
  name: string;
  email: string;
  role: RoleOption;
  capabilities: string[];
  channel: InviteChannel;
}): Promise<InviteCreated> {
  return fetchAndParse(inviteCreatedSchema, "/api/v1/household/members/invite", {
    method: "POST",
    body: JSON.stringify(vars),
  });
}

export function useInviteMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: inviteMember,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: householdMembersQueryKey });
      queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey });
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}
