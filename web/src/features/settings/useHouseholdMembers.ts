// The household's member list, in one place. Three screens declared this same
// query privately -- AccountModal (to pick an account's owner), TransactionsPage
// (to filter by who paid) and MembersPanel (to list and edit them) -- against
// the same ["household", "members"] key, so they already shared a cache entry
// by coincidence rather than by construction. Overview's setup checklist would
// have been a fourth copy; this is that copy not being written.
//
// Every write that changes the list (useUpdateMember.ts, useInviteMember.ts)
// invalidates through this constant, never a literal copy, so the key can
// change here without silently stranding a refresh. It must still start with
// householdQueryKey (useHousehold.ts): PATCH /household invalidates by that
// prefix and relies on it reaching this list too.
import { useQuery } from "@tanstack/react-query";
import { fetchAndParse } from "../../api/client";
import { membersListSchema, type MemberView } from "./schemas";

export const householdMembersQueryKey = ["household", "members"] as const;

async function fetchHouseholdMembers(): Promise<MemberView[]> {
  return fetchAndParse(membersListSchema, "/api/v1/household/members");
}

export function useHouseholdMembers() {
  return useQuery({ queryKey: householdMembersQueryKey, queryFn: fetchHouseholdMembers });
}
