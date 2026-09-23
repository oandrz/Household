// The household access list (Settings' Access panel) and the two token
// writes it offers. Both writes go to the existing /auth/tokens routes,
// which only ever touch the caller's own tokens -- this file adds no power
// the API did not already give (spec decision 6).
//
// The key starts with "household" for the reason pendingInvitesQueryKey's
// does: PATCH /household invalidates by that prefix.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, fetchAndParse } from "../../api/client";
import {
  createdApiTokenSchema,
  householdAccessSchema,
  type CreatedApiToken,
  type HouseholdAccess,
} from "./schemas";

export const householdAccessQueryKey = ["household", "access"] as const;

async function fetchHouseholdAccess(): Promise<HouseholdAccess> {
  return fetchAndParse(householdAccessSchema, "/api/v1/household/access");
}

export function useHouseholdAccess() {
  return useQuery({ queryKey: householdAccessQueryKey, queryFn: fetchHouseholdAccess });
}

export type NewApiToken = { name: string; expiresInDays: number };

export function useCreateApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    // apiFetch sets Content-Type: application/json whenever a body is given.
    mutationFn: async (input: NewApiToken): Promise<CreatedApiToken> =>
      fetchAndParse(createdApiTokenSchema, "/api/v1/auth/tokens", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: householdAccessQueryKey }),
  });
}

export function useRevokeApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    // 204, nothing to parse.
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/auth/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
    },
    // onSettled: a 404 means the token was already revoked in another tab,
    // and the row on screen is stale either way. Returned so Revoke stays
    // disabled until the refetch lands.
    onSettled: () => queryClient.invalidateQueries({ queryKey: householdAccessQueryKey }),
  });
}
