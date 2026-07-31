// The household's member list, in one place. Three screens declared this same
// query privately -- AccountModal (to pick an account's owner), TransactionsPage
// (to filter by who paid) and MembersPanel (to list and edit them) -- against
// the same ["household", "members"] key, so they already shared a cache entry
// by coincidence rather than by construction. Overview's setup checklist would
// have been a fourth copy; this is that copy not being written.
//
// The key stays exactly ["household", "members"]: MembersPanel's mutations
// invalidate it by that literal, and changing it here would silently stop a
// role change from refreshing the list.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { membersListSchema, type MemberView } from "./schemas";

export const householdMembersQueryKey = ["household", "members"] as const;

async function fetchHouseholdMembers(): Promise<MemberView[]> {
  const body = await apiFetch<unknown>("/api/v1/household/members");
  return membersListSchema.parse(body);
}

export function useHouseholdMembers() {
  return useQuery({ queryKey: householdMembersQueryKey, queryFn: fetchHouseholdMembers });
}
