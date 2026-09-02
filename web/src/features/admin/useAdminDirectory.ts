// Query hooks over the directory routes (api/internal/adapter/http/
// admin_directory_handlers.go). Same shape as useAdmin.ts's useAdminFlags,
// same two rules: refetchOnWindowFocus is off because every request under
// /admin is an audit row, and a lapsed grant is not handled here -- it is
// routed to the one AdminGate AdminShell owns (useCloseSurfaceOnReauth).
import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { adminFlagsKey } from "./useAdmin";
import {
  adminHouseholdPageSchema,
  adminHouseholdsResponseSchema,
  type AdminHouseholdPage,
  type AdminHouseholdsResponse,
} from "./adminDirectorySchemas";

export const DIRECTORY_DEFAULT_LIMIT = 50;
export const DIRECTORY_MAX_LIMIT = 200;

// adminHouseholdsPath builds the exact URL the page requests, exported so a
// test's fetch stub and the hook agree byte for byte. q is omitted when
// empty so the audit row for a plain page view stays {}.
export function adminHouseholdsPath(q: string, limit: number): string {
  const params = new URLSearchParams();
  if (q !== "") params.set("q", q);
  params.set("limit", String(limit));
  return `/api/v1/admin/households?${params.toString()}`;
}

export function adminHouseholdsKey(q: string, limit: number) {
  return ["admin", "households", { q, limit }] as const;
}

export function adminHouseholdKey(householdId: string) {
  return ["admin", "household", householdId] as const;
}

async function fetchAdminHouseholds(
  q: string,
  limit: number,
): Promise<AdminHouseholdsResponse> {
  const body = await apiFetch<unknown>(adminHouseholdsPath(q, limit));
  return adminHouseholdsResponseSchema.parse(body);
}

async function fetchAdminHousehold(
  householdId: string,
): Promise<AdminHouseholdPage> {
  const body = await apiFetch<unknown>(
    `/api/v1/admin/households/${encodeURIComponent(householdId)}`,
  );
  return adminHouseholdPageSchema.parse(body);
}

export function useAdminHouseholds(q: string, limit: number) {
  return useQuery({
    queryKey: adminHouseholdsKey(q, limit),
    queryFn: () => fetchAdminHouseholds(q, limit),
    refetchOnWindowFocus: false,
  });
}

export function useAdminHousehold(householdId: string) {
  return useQuery({
    queryKey: adminHouseholdKey(householdId),
    queryFn: () => fetchAdminHousehold(householdId),
    refetchOnWindowFocus: false,
  });
}

// useCloseSurfaceOnReauth: a page-level query that meets a lapsed grant
// does not render its own prompt. Invalidating adminFlagsKey makes the
// query AdminShell's gate already watches refetch, hit the same 401, and
// close the whole surface -- the identical route useAdmin.ts's write
// mutations take. NOT_FOUND is deliberately not here: on the drill-in a
// 404 means "no such household" and is the page's own to render.
export function useCloseSurfaceOnReauth(error: unknown): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (error instanceof ApiError && error.code === "ADMIN_REAUTH_REQUIRED") {
      queryClient.invalidateQueries({ queryKey: adminFlagsKey });
    }
  }, [error, queryClient]);
}

export function isNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}
