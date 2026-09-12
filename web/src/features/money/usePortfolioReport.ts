// Fetch orchestration for the period report, on TanStack Query -- the
// useHoldings.ts/useGoals.ts house pattern.
//
// The window length is NOT defaulted here. The server picks it per period kind
// (6 quarters, 4 halves, 3 years) and a default in the browser would be a
// second copy of that rule, free to drift from the one the chart's bar budget
// was actually chosen against. `count` is passed only when a caller genuinely
// wants something other than the server's answer.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { portfolioReportSchema, type PeriodKind, type PortfolioReport } from "./holdingSchemas";

export function portfolioReportQueryKey(kind: PeriodKind, count?: number) {
  return ["portfolio-report", { kind, count: count ?? null }] as const;
}

async function fetchReport(kind: PeriodKind, count?: number): Promise<PortfolioReport> {
  const query = new URLSearchParams({ kind });
  if (count !== undefined) query.set("count", String(count));
  const body = await apiFetch<unknown>(`/api/v1/holdings/report?${query.toString()}`);
  return portfolioReportSchema.parse(body);
}

export function usePortfolioReport(options: { kind: PeriodKind; count?: number; enabled?: boolean }) {
  const { kind, count, enabled = true } = options;
  return useQuery({
    queryKey: portfolioReportQueryKey(kind, count),
    queryFn: () => fetchReport(kind, count),
    enabled,
  });
}
