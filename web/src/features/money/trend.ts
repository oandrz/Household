// Pure trend logic, kept out of NetWorthChart.tsx itself: react-refresh's own
// lint rule (eslint.config.js's reactRefresh.configs.vite) forbids a
// component file from exporting anything besides components, and
// FinancesPage.tsx needs this exact rule -- not a second, independently
// written one -- to decide whether to mount the chart at all.
import type { TrendPoint } from "./schemas";

// Fewer than two known months is not a trend. A brand-new household has
// every account opened today, so it has exactly one -- and a single bar
// pinned to the right-hand edge with eleven empty slots beside it says less
// than the sentence does.
//
// Two callers share this one definition: NetWorthChart.tsx uses it to choose
// between drawing bars and its own "not enough history" text, and
// FinancesPage.tsx uses it to decide whether to mount the chart at all and
// whether to show the "Last 12 months" heading beside it. A household with
// exactly one account -- a real first day, since FirstRunPanel only
// pre-empts zero accounts -- depends on both callers agreeing, which a
// second, separately written `>= 2` check could not guarantee.
export function hasDrawableTrend(points: TrendPoint[]): boolean {
  return points.filter((point) => point.netWorthMinor !== null).length >= 2;
}
