// Fetch orchestration for NotificationsPanel: GET and PATCH
// /notification-preferences.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchAndParse } from "../../api/client";
import { meQueryKey } from "../auth/useAuth";
import {
  notificationPreferencesSchema,
  type NotificationPreferences,
} from "./schemas";

const preferencesQueryKey = ["notification-preferences"] as const;

async function fetchPreferences(): Promise<NotificationPreferences> {
  return fetchAndParse(notificationPreferencesSchema, "/api/v1/notification-preferences");
}

export function usePreferences() {
  return useQuery({ queryKey: preferencesQueryKey, queryFn: fetchPreferences });
}

// One field per call, a genuine partial PATCH -- the server applies only
// the keys present in the body and leaves every omitted toggle untouched
// (see notificationPreferencesRequest's pointer fields in
// household_handlers.go), so a single toggle click never risks touching
// the other three.
export function useUpdatePreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      vars: Partial<NotificationPreferences>,
    ): Promise<NotificationPreferences> => {
      return fetchAndParse(notificationPreferencesSchema, "/api/v1/notification-preferences", {
        method: "PATCH",
        body: JSON.stringify(vars),
      });
    },
    // Returns (rather than fires-and-forgets) the invalidation promises --
    // see useHousehold.ts's useUpdateHousehold for the identical fix and
    // the full reasoning. Here it matters even though every toggle patches
    // a different field: two rapid clicks on the *same* toggle (on, then
    // off again) would otherwise both compute from the identical stale
    // cached value once `isPending` cleared early, sending the same
    // request twice instead of the second, intended reversal.
    onSuccess: () => {
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: preferencesQueryKey }),
        queryClient.invalidateQueries({ queryKey: meQueryKey }),
      ]);
    },
  });
}
