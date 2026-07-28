import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { accountsResponseSchema, type AccountsResponse } from "./schemas";

export function accountsQueryKey(includeArchived: boolean) {
  return ["accounts", { includeArchived }] as const;
}

async function fetchAccounts(includeArchived: boolean): Promise<AccountsResponse> {
  const suffix = includeArchived ? "?include_archived=true" : "";
  const body = await apiFetch<unknown>(`/api/v1/accounts${suffix}`);
  return accountsResponseSchema.parse(body);
}

export function useAccounts(includeArchived: boolean) {
  return useQuery({
    queryKey: accountsQueryKey(includeArchived),
    queryFn: () => fetchAccounts(includeArchived),
  });
}
