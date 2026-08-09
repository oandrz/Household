// Fetch orchestration for the Bills screen, built on TanStack Query -- the
// same house pattern useAccounts.ts/useBudget.ts use, not a hand-rolled
// useState/useEffect pair (see useBudget.ts's own header comment for why
// that shape was rejected once already, and why it exists as its own file
// rather than living inline in BillsPage.tsx: BudgetPage.tsx never grew the
// debt TransactionsPage.tsx carries -- over 500 lines doing fetch
// orchestration, pagination, body translation and row rendering together --
// because useBudget.ts existed from day one). No `apiFetch` call may appear
// in a component in this feature; every one lives here.
//
// Shaped like useAccounts.ts (a query hook plus one exported hook per
// mutation, each returning the raw useMutation object), not useGoals.ts's
// single hook exposing every write as a method. bill_handlers.go's own
// comments say why: writeBill hands back the complete BillView on every
// write specifically "so this handler never needs a second Get" -- Create,
// Update, Archive and Restore all parse and return the written Bill for the
// same reason a caller here might want it immediately, the way
// useAccounts.ts's useCreateAccount/useUpdateAccount/useSetAccountArchived
// already do. Every mutation still invalidates both `includeArchived`
// variants on success regardless -- the returned Bill has no aggregate
// figures (billsSummaryDTO's counts, paidThisMonth), so only a refetch of
// the list keeps those true.
//
// useBills itself is the one place this file borrows from useBudget.ts
// instead: its own `enabled` option, not anything useAccounts.ts carries --
// see useBills's own comment below for why GET /bills needs it and GET
// /accounts doesn't.
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  billPaymentResponseSchema,
  billResponseSchema,
  billsResponseSchema,
  type Bill,
  type BillPayment,
  type BillsResponse,
} from "./billSchemas";

// CreateBillBody mirrors createBillRequest (bill_handlers.go). Every field
// is required and sent as typed -- there is no currency field because a
// bill's currency is never chosen directly, only inherited from
// payFromAccountId's own account (createBillRequest carries no such field
// either). nextDue is a required string, not `| null`: a bill can only ever
// reach nextDue === null by being settled through MarkPaid
// (createBillRequest's own comment) -- there is no way to create one
// already in that state.
export type CreateBillBody = {
  name: string;
  amountMinor: number;
  cadence: "one_off" | "monthly" | "quarterly" | "yearly";
  nextDue: string;
  categoryId: string;
  payFromAccountId: string;
  paidByMembershipId: string;
  autopay: boolean;
  isSubscription: boolean;
};

// UpdateBillBody mirrors updateBillRequest. Every field is optional so an
// absent key round-trips as "unchanged" (usecase.BillPatch's own
// convention, the same reasoning UpdateGoalBody's own comment gives) --
// JSON.stringify drops an `undefined` value's key entirely, which is what
// makes a plain TypeScript `?:` field the right mirror of a Go pointer
// field read this way. clearCategory/clearPayer are how a caller unsets
// categoryId/paidByMembershipId, mirroring BillPatch's own explicit-clear
// fields: a nil categoryId already means "leave alone", so it cannot also
// mean "clear" (the identical reasoning updateBillRequest's own comment
// gives). Archiving is deliberately absent -- it is not a patchable field
// (its own dedicated archive/restore routes exist so an ordinary rename
// never archives a bill as a side effect of saving).
export type UpdateBillBody = {
  name?: string;
  amountMinor?: number;
  cadence?: "one_off" | "monthly" | "quarterly" | "yearly";
  nextDue?: string;
  categoryId?: string;
  clearCategory?: boolean;
  payFromAccountId?: string;
  paidByMembershipId?: string;
  clearPayer?: boolean;
  autopay?: boolean;
  isSubscription?: boolean;
};

// PayBillBody mirrors payBillRequest. amountMinor is optional, not a plain
// number: omitted means "the bill's own stored amount" (payBillRequest's
// own comment on its default), and leaving it out is the only way to reach
// that default from JSON.stringify -- an explicit `amountMinor: 0` must
// still reach the server as a genuine bad request. paidOn carries no
// default of its own on this side either: the brief gives one for the
// amount and pointedly not for the date, so a caller that has no date yet
// must not send one just to satisfy this type.
export type PayBillBody = {
  amountMinor?: number;
  paidOn: string;
};

export function billsQueryKey(includeArchived: boolean) {
  return ["bills", { includeArchived }] as const;
}

async function fetchBills(includeArchived: boolean): Promise<BillsResponse> {
  const suffix = includeArchived ? "?include_archived=true" : "";
  const body = await apiFetch<unknown>(`/api/v1/bills${suffix}`);
  return billsResponseSchema.parse(body);
}

// `enabled` exists for Overview (Task 16), which renders for every member
// but may only read bills for an owner (GET /bills is requireCapability
// (money) AND requireOwner, task-9-report.md's own note). A caller cannot
// skip the hook -- that breaks the rules of hooks -- and firing the request
// anyway would both cost a doomed 403 and cache that failure under
// `billsQueryKey(includeArchived)`, poisoning the same key BillsPage itself
// reads next. useBudget.ts's own `enabled` carries the identical reasoning
// for the identical money+owner gate; useAccounts.ts has no such option
// because GET /accounts is money-only (a limited member gets a real body
// with `summary` omitted, never a 403), so it was never the right model for
// this one option even though it is for every mutation shape below.
export function useBills(includeArchived: boolean, options: { enabled?: boolean } = {}) {
  const enabled = options.enabled ?? true;
  return useQuery({
    queryKey: billsQueryKey(includeArchived),
    queryFn: () => fetchBills(includeArchived),
    enabled,
  });
}

// Both billsQueryKey variants, the same useAccounts.ts invalidateAccounts
// shape and for the same reason: a write performed while the screen shows
// one variant (live only, or live-and-archived) must not leave the other
// stale for the next time "Show archived" is toggled. Returned (not
// fired-and-forgotten) from every mutation below -- TanStack Query awaits
// the callback's return value before treating the mutation as settled, and
// settled is what a caller's `await ...mutateAsync(...)` actually waits on.
function invalidateBills(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: billsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: billsQueryKey(true) }),
  ]);
}

export function useCreateBill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: CreateBillBody): Promise<Bill> => {
      const raw = await apiFetch<unknown>("/api/v1/bills", {
        method: "POST",
        body: JSON.stringify(body),
      });
      return billResponseSchema.parse(raw).bill;
    },
    onSuccess: () => invalidateBills(queryClient),
  });
}

export function useUpdateBill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { id: string; body: UpdateBillBody }): Promise<Bill> => {
      const raw = await apiFetch<unknown>(`/api/v1/bills/${encodeURIComponent(vars.id)}`, {
        method: "PATCH",
        body: JSON.stringify(vars.body),
      });
      return billResponseSchema.parse(raw).bill;
    },
    onSuccess: () => invalidateBills(queryClient),
  });
}

export function useArchiveBill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<Bill> => {
      const raw = await apiFetch<unknown>(`/api/v1/bills/${encodeURIComponent(id)}/archive`, {
        method: "POST",
      });
      return billResponseSchema.parse(raw).bill;
    },
    onSuccess: () => invalidateBills(queryClient),
  });
}

export function useRestoreBill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<Bill> => {
      const raw = await apiFetch<unknown>(`/api/v1/bills/${encodeURIComponent(id)}/restore`, {
        method: "POST",
      });
      return billResponseSchema.parse(raw).bill;
    },
    onSuccess: () => invalidateBills(queryClient),
  });
}

// useMarkPaid parses and returns both halves of billPaymentResponseSchema
// -- billPaymentResponse's own comment says the bill half already carries
// the full joined view (nextDue advanced, or settled) specifically so a
// caller never needs a second GET to show what paying just changed.
export function useMarkPaid() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { id: string; body: PayBillBody }): Promise<{ payment: BillPayment; bill: Bill }> => {
      const raw = await apiFetch<unknown>(`/api/v1/bills/${encodeURIComponent(vars.id)}/pay`, {
        method: "POST",
        body: JSON.stringify(vars.body),
      });
      return billPaymentResponseSchema.parse(raw);
    },
    onSuccess: () => invalidateBills(queryClient),
  });
}

// 204 with no body -- the one status apiFetch does not try to parse
// (handleUndoBillPayment's own comment). mutationFn's only job is the
// DELETE call itself, the same shape useGoals.ts's deleteContribution and
// useTransactions.ts's useDeleteTransaction both use for their own 204s.
export function useUndoPayment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { billId: string; paymentId: string }): Promise<void> => {
      await apiFetch<unknown>(
        `/api/v1/bills/${encodeURIComponent(vars.billId)}/payments/${encodeURIComponent(vars.paymentId)}`,
        { method: "DELETE" },
      );
    },
    onSuccess: () => invalidateBills(queryClient),
  });
}
