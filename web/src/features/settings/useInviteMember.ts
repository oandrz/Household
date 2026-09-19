// Fetch orchestration for InviteMemberModal: POST /household/members/invite.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { meQueryKey } from "../auth/useAuth";
import { householdMembersQueryKey } from "./useHouseholdMembers";

export type RoleOption = "owner" | "limited";

export const ROLE_OPTIONS: readonly RoleOption[] = ["owner", "limited"];

async function inviteMember(vars: {
  name: string;
  email: string;
  role: RoleOption;
  capabilities: string[];
}): Promise<{ status: string }> {
  return apiFetch<{ status: string }>("/api/v1/household/members/invite", {
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
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}
