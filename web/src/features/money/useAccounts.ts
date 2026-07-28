import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { accountSchema, accountsResponseSchema, type Account, type AccountsResponse } from "./schemas";
import type { AccountEditValues, AccountFormValues } from "./AccountModal";

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

// Both keys: the panel may be showing either the live list or the archived
// one, and an archive performed from one must not leave the other stale.
// Returned (not fired-and-forgotten) by every mutation below -- TanStack
// Query awaits a mutation's onSuccess return value before treating it as
// settled, and settled is what isPending (every submit button's disabled
// condition here) reflects. Skipping the `return` re-enables the button
// while the list is still serving its stale cached value -- the defect
// CurrencyPanel documents at web/src/features/settings/CurrencyPanel.tsx:49.
function invalidateAccounts(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(true) }),
  ]);
}

export function useCreateAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: AccountFormValues): Promise<Account> => {
      const raw = await apiFetch<unknown>("/api/v1/accounts", {
        method: "POST",
        body: JSON.stringify(body),
      });
      return accountSchema.parse(raw);
    },
    onSuccess: () => invalidateAccounts(queryClient),
  });
}

export function useUpdateAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    // AccountEditValues, not AccountFormValues: openingBalanceMinor/
    // openingBalanceCurrency are only present on `vars` when AccountModal
    // actually saw Balance or Currency touched. Spreading `body` below keeps
    // that absence intact all the way to JSON.stringify -- a field this
    // object never had is a field the request never mentions, which is what
    // lets usecase.AccountUpdate's nil-means-unchanged handling leave the
    // stored balance exactly as it was.
    mutationFn: async (vars: { id: string } & AccountEditValues): Promise<Account> => {
      const { id, ...body } = vars;
      const payload = {
        ...body,
        // updateAccountRequest.OwnerMembershipID is a *string that means
        // "leave the owner alone" when nil -- and a JSON `null` decodes to
        // that same nil, indistinguishable from the field being absent
        // (account_handlers.go / usecase.AccountUpdate's own doc comment).
        // Only a pointer to "" clears the owner to Shared. AccountFormValues
        // models Shared as `null` (matching the create route, where null and
        // omission both default to shared), so it has to be translated to ""
        // here -- otherwise switching an owned account back to Shared during
        // an edit would silently leave the previous owner in place.
        ownerMembershipId: body.ownerMembershipId ?? "",
      };
      const raw = await apiFetch<unknown>(`/api/v1/accounts/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(payload),
      });
      return accountSchema.parse(raw);
    },
    onSuccess: () => invalidateAccounts(queryClient),
  });
}

export function useSetAccountArchived() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { id: string; archived: boolean }): Promise<Account> => {
      const suffix = vars.archived ? "archive" : "restore";
      const raw = await apiFetch<unknown>(
        `/api/v1/accounts/${encodeURIComponent(vars.id)}/${suffix}`,
        { method: "POST" },
      );
      return accountSchema.parse(raw);
    },
    onSuccess: () => invalidateAccounts(queryClient),
  });
}
