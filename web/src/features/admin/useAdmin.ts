// Query and mutation hooks over the admin routes (api/internal/adapter/http/
// admin_handlers.go). Every one of these can answer 401 ADMIN_REAUTH_REQUIRED
// at any moment -- the grant is a wall-clock TTL, not one activity extends
// (middleware_admin.go's own adminGrantTTL comment) -- so nothing here tries
// to react to that itself. AdminShell owns the one AdminGate that decides
// what the whole surface shows; see toAdminGateError below for how a raw
// hook error becomes the ApiError | null that component switches on, and
// each write mutation's onError for how a lapsed grant discovered mid-edit
// still reaches that same gate.
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { adminFlagsResponseSchema, type AdminFlag, type AdminFlagsResponse } from "./adminSchemas";

export const adminFlagsKey = ["admin", "flags"] as const;

export type { AdminFlag };

// toAdminGateError narrows a hook's `error` (typed as plain `Error | null` by
// TanStack Query) into what AdminGate switches on. A failure that isn't an
// ApiError -- a network TypeError, a body that failed
// adminFlagsResponseSchema.parse -- carries none of the three codes AdminGate
// recognises, and treating it as "no error" would open the real admin
// surface on a failure nobody has actually authorised. Wrapping it in an
// ApiError whose code matches nothing keeps it on AdminGate's own
// fail-closed default (see AdminGate.tsx) instead of falling through as
// null.
export function toAdminGateError(error: unknown): ApiError | null {
  if (!error) return null;
  if (error instanceof ApiError) return error;
  return new ApiError(0, "UNKNOWN", "Something went wrong loading the admin surface.");
}

async function fetchAdminFlags(): Promise<AdminFlagsResponse> {
  const body = await apiFetch<unknown>("/api/v1/admin/flags");
  return adminFlagsResponseSchema.parse(body);
}

export function useAdminFlags() {
  return useQuery({ queryKey: adminFlagsKey, queryFn: fetchAdminFlags });
}

// useAdminSession is the re-authentication itself -- AdminGate's password
// prompt calls its `mutate`, and its `error` is what tells AdminShell a
// submitted password was wrong (401 INVALID_CREDENTIALS) or that the surface
// just locked (423 ADMIN_LOCKED), neither of which touches adminFlagsKey's
// own cache entry since they come from a different endpoint than the query
// above. Its success invalidates that cache entry so the flags list
// (blocked on the grant a moment ago) is fetched for the first time.
export function useAdminSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (password: string) =>
      apiFetch<void>("/api/v1/admin/session", {
        method: "POST",
        body: JSON.stringify({ password }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminFlagsKey }),
  });
}

// A write mutation's own onSuccess primes adminFlagsKey directly from its
// response, matching handleSetGlobalFlag/handleSetHouseholdFlag/
// handleClearHouseholdFlag's own "answer the whole refreshed list" contract
// -- the screen never has to guess what the write did to any flag's
// effective value.
function cacheRefreshedFlags(queryClient: QueryClient) {
  return (data: AdminFlagsResponse) => queryClient.setQueryData(adminFlagsKey, data);
}

// A write's own onError, shared by all three mutations below. It does not
// render anything itself: ADMIN_REAUTH_REQUIRED (the grant lapsed since the
// page opened) and NOT_FOUND (platform-admin revoked mid-session) are both
// signals the *whole* surface should close, not something one row's mutation
// should show inline -- invalidating adminFlagsKey makes the query AdminShell's
// gate already watches refetch, hit the same failure, and close it through
// the one AdminGate this file's header comment points to. Any other failure
// (an unknown flag key, a lookup error) is left for the caller's own
// mutation.error to render next to the control that triggered it.
// Typed to accept `Error`, matching TanStack Query's own default TError --
// not `unknown`, which would widen every write mutation's own inferred
// TError to `unknown` too (TError is inferred from every callback passed to
// `useMutation`, this one included) and force every caller of
// `mutation.error` to narrow it back down themselves.
function closeSurfaceOnAdminLayerFailure(queryClient: QueryClient) {
  return (error: Error) => {
    if (error instanceof ApiError && (error.code === "ADMIN_REAUTH_REQUIRED" || error.code === "NOT_FOUND")) {
      queryClient.invalidateQueries({ queryKey: adminFlagsKey });
    }
  };
}

async function putFlag(path: string, enabled: boolean): Promise<AdminFlagsResponse> {
  const body = await apiFetch<unknown>(path, {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  });
  return adminFlagsResponseSchema.parse(body);
}

export function useSetGlobalFlag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: { key: string; enabled: boolean }) =>
      putFlag(`/api/v1/admin/flags/${encodeURIComponent(vars.key)}`, vars.enabled),
    onSuccess: cacheRefreshedFlags(queryClient),
    onError: closeSurfaceOnAdminLayerFailure(queryClient),
  });
}

export function useSetHouseholdFlag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: { key: string; householdId: string; enabled: boolean }) =>
      putFlag(
        `/api/v1/admin/flags/${encodeURIComponent(vars.key)}/households/${encodeURIComponent(vars.householdId)}`,
        vars.enabled,
      ),
    onSuccess: cacheRefreshedFlags(queryClient),
    onError: closeSurfaceOnAdminLayerFailure(queryClient),
  });
}

// ClearHouseholdFlag is DELETE, not PUT enabled:false -- "no opinion" and
// "explicitly off" are different states (handleClearHouseholdFlag's own
// comment), and this is the only one of the three write hooks that reaches
// it.
export function useClearHouseholdFlag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { key: string; householdId: string }) => {
      const body = await apiFetch<unknown>(
        `/api/v1/admin/flags/${encodeURIComponent(vars.key)}/households/${encodeURIComponent(vars.householdId)}`,
        { method: "DELETE" },
      );
      return adminFlagsResponseSchema.parse(body);
    },
    onSuccess: cacheRefreshedFlags(queryClient),
    onError: closeSurfaceOnAdminLayerFailure(queryClient),
  });
}
