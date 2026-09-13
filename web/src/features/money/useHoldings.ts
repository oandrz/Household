// Fetch orchestration for the Portfolio screen, on TanStack Query -- the same
// house pattern useGoals.ts/useAccounts.ts use. Holdings share useGoals.ts's
// include_archived union shape (live-and-archived from one endpoint, toggled
// by a query parameter), so holdingsQueryKey is modelled on goalsQueryKey.
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch, fetchAndParse } from "../../api/client";
import {
  holdingEventsResponseSchema,
  holdingIncomeResponseSchema,
  holdingValuationsResponseSchema,
  portfolioResponseSchema,
  type PortfolioResponse,
  type InstrumentKind,
  type HoldingEventKind,
  type IncomeKind,
} from "./holdingSchemas";

// CreateHoldingBody mirrors createHoldingRequest. currency is optional: the
// server falls back to the household's primary when it is absent, so the modal
// does not have to know it.
export type CreateHoldingBody = {
  accountId: string;
  name: string;
  instrument: InstrumentKind;
  unit: string;
  currency?: string;
};

export type UpdateHoldingBody = {
  name: string;
  instrument: InstrumentKind;
  unit: string;
};

// RecordEventBody mirrors createHoldingEventRequest. `quantity` is a STRING --
// exactly what the person typed, sent verbatim. Converting it to nano units
// here would put the 1e9 division on the client, which is the float64 defect
// holdingSchemas.ts's own comment names.
type RecordEventBody = {
  kind: HoldingEventKind;
  quantity: string;
  amountMinor: number;
  primaryAmountMinor?: number;
  occurredOn: string;
  note: string;
};

// RecordIncomeBody mirrors createIncomeRequest. A fee is sent POSITIVE, the
// same as a dividend: the server subtracts fees when it sums a period, and a
// negative amount is refused everywhere in this product.
type RecordIncomeBody = {
  kind: IncomeKind;
  amountMinor: number;
  primaryAmountMinor?: number;
  receivedOn: string;
  note: string;
};

type RecordValuationBody = {
  unitPriceMinor: number;
  primaryUnitPriceMinor?: number;
  asOf: string;
  note: string;
};

function holdingsQueryKey(includeArchived: boolean) {
  return ["holdings", { includeArchived }] as const;
}

function holdingEventsQueryKey(holdingId: string) {
  return ["holding-events", holdingId] as const;
}

function holdingValuationsQueryKey(holdingId: string) {
  return ["holding-valuations", holdingId] as const;
}

function holdingIncomeQueryKey(holdingId: string) {
  return ["holding-income", holdingId] as const;
}

async function fetchPortfolio(includeArchived: boolean): Promise<PortfolioResponse> {
  const suffix = includeArchived ? "?include_archived=true" : "";
  return fetchAndParse(portfolioResponseSchema, `/api/v1/holdings${suffix}`);
}

// Both variants, the useGoals.ts invalidateGoals shape and for its reason: a
// write performed while the screen shows one (live only, or live-and-archived)
// must not leave the other stale for the next toggle.
//
// The period report too, for the reason invalidateAfterEventWrite gives at
// length: it lists every holding the household has and prints each one's name,
// so a holding added or renamed here changes what that screen should say even
// though no figure on it moved.
function invalidateHoldings(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: holdingsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: holdingsQueryKey(true) }),
    queryClient.invalidateQueries({ queryKey: ["portfolio-report"] }),
  ]);
}

// An event or a valuation moves the holding's own figures AND the list's, so
// both are invalidated together.
//
// The PERIOD REPORT is invalidated here too, and it is the one that is easy to
// forget: it lives on its own query key, so nothing about refetching the
// portfolio reaches it. Without this line the sequence the owner actually
// performs -- open the report, see "no price recorded in this period", go back
// and record one, return -- serves the cached report and still says no price.
// The feature would look broken while being correct on the server.
function invalidateAfterEventWrite(queryClient: QueryClient, holdingId: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: holdingsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: holdingsQueryKey(true) }),
    queryClient.invalidateQueries({ queryKey: holdingEventsQueryKey(holdingId) }),
    queryClient.invalidateQueries({ queryKey: holdingValuationsQueryKey(holdingId) }),
    queryClient.invalidateQueries({ queryKey: ["portfolio-report"] }),
  ]);
}

// An income write changes no position -- income never enters the fold -- so it
// invalidates its own list and the period report, and leaves the portfolio's
// held/cost/realised figures alone. Invalidating those too would refetch the
// whole screen to change nothing on it.
function invalidateAfterIncomeWrite(queryClient: QueryClient, holdingId: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: holdingIncomeQueryKey(holdingId) }),
    queryClient.invalidateQueries({ queryKey: ["portfolio-report"] }),
  ]);
}

export function useHoldings(options: { includeArchived: boolean; enabled?: boolean }) {
  const { includeArchived, enabled = true } = options;
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: holdingsQueryKey(includeArchived),
    queryFn: () => fetchPortfolio(includeArchived),
    enabled,
  });

  // Every write invalidates and refetches rather than patching local state
  // from its own response: the portfolio is where the fold, the price and the
  // position are computed together, and re-deriving them here would duplicate
  // HoldingService.Portfolio's own arithmetic on the client -- useGoals.ts's
  // reasoning, and the same risk.
  const createHolding = useMutation({
    mutationFn: (body: CreateHoldingBody) =>
      apiFetch<unknown>("/api/v1/holdings", { method: "POST", body: JSON.stringify(body) }),
    onSuccess: () => invalidateHoldings(queryClient),
  });

  const updateHolding = useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateHoldingBody }) =>
      apiFetch<unknown>(`/api/v1/holdings/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    onSuccess: () => invalidateHoldings(queryClient),
  });

  const archiveHolding = useMutation({
    mutationFn: (id: string) =>
      apiFetch<unknown>(`/api/v1/holdings/${encodeURIComponent(id)}/archive`, { method: "POST" }),
    onSuccess: () => invalidateHoldings(queryClient),
  });

  const restoreHolding = useMutation({
    mutationFn: (id: string) =>
      apiFetch<unknown>(`/api/v1/holdings/${encodeURIComponent(id)}/restore`, { method: "POST" }),
    onSuccess: () => invalidateHoldings(queryClient),
  });

  const recordEvent = useMutation({
    mutationFn: ({ id, body }: { id: string; body: RecordEventBody }) =>
      apiFetch<unknown>(`/api/v1/holdings/${encodeURIComponent(id)}/events`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (_data, variables) => invalidateAfterEventWrite(queryClient, variables.id),
  });

  const deleteEvent = useMutation({
    mutationFn: ({ id, eventId }: { id: string; eventId: string }) =>
      apiFetch<unknown>(
        `/api/v1/holdings/${encodeURIComponent(id)}/events/${encodeURIComponent(eventId)}`,
        { method: "DELETE" },
      ),
    onSuccess: (_data, variables) => invalidateAfterEventWrite(queryClient, variables.id),
  });

  const recordValuation = useMutation({
    mutationFn: ({ id, body }: { id: string; body: RecordValuationBody }) =>
      apiFetch<unknown>(`/api/v1/holdings/${encodeURIComponent(id)}/valuations`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (_data, variables) => invalidateAfterEventWrite(queryClient, variables.id),
  });

  const recordIncome = useMutation({
    mutationFn: ({ id, body }: { id: string; body: RecordIncomeBody }) =>
      apiFetch<unknown>(`/api/v1/holdings/${encodeURIComponent(id)}/income`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (_data, variables) => invalidateAfterIncomeWrite(queryClient, variables.id),
  });

  const deleteIncome = useMutation({
    mutationFn: ({ id, incomeId }: { id: string; incomeId: string }) =>
      apiFetch<unknown>(
        `/api/v1/holdings/${encodeURIComponent(id)}/income/${encodeURIComponent(incomeId)}`,
        { method: "DELETE" },
      ),
    onSuccess: (_data, variables) => invalidateAfterIncomeWrite(queryClient, variables.id),
  });

  return {
    ...query,
    createHolding,
    updateHolding,
    archiveHolding,
    restoreHolding,
    recordEvent,
    deleteEvent,
    recordValuation,
    recordIncome,
    deleteIncome,
  };
}

export function useHoldingEvents(holdingId: string | null) {
  return useQuery({
    queryKey: holdingEventsQueryKey(holdingId ?? ""),
    queryFn: async () => {
      return fetchAndParse(
        holdingEventsResponseSchema,
        `/api/v1/holdings/${encodeURIComponent(holdingId ?? "")}/events`,
      );
    },
    enabled: holdingId !== null,
  });
}

export function useHoldingValuations(holdingId: string | null) {
  return useQuery({
    queryKey: holdingValuationsQueryKey(holdingId ?? ""),
    queryFn: async () => {
      return fetchAndParse(
        holdingValuationsResponseSchema,
        `/api/v1/holdings/${encodeURIComponent(holdingId ?? "")}/valuations`,
      );
    },
    enabled: holdingId !== null,
  });
}

export function useHoldingIncome(holdingId: string | null) {
  return useQuery({
    queryKey: holdingIncomeQueryKey(holdingId ?? ""),
    queryFn: async () => {
      return fetchAndParse(
        holdingIncomeResponseSchema,
        `/api/v1/holdings/${encodeURIComponent(holdingId ?? "")}/income`,
      );
    },
    enabled: holdingId !== null,
  });
}
