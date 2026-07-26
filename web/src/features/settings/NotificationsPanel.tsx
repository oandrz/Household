// The Notifications panel (design/Household Dashboard.dc.html's Settings
// screen, the full-width card below the grid). Four household-wide toggles,
// owner-only to change (PATCH /notification-preferences sits behind
// requireOwner -- these are not a per-member preference, see
// household_handlers.go's doc comment on handleUpdateNotificationPreferences).
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { useMe } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import {
  notificationPreferencesSchema,
  type NotificationPreferences,
} from "./schemas";

const preferencesQueryKey = ["notification-preferences"] as const;

async function fetchPreferences(): Promise<NotificationPreferences> {
  const body = await apiFetch<unknown>("/api/v1/notification-preferences");
  return notificationPreferencesSchema.parse(body);
}

function usePreferences() {
  return useQuery({ queryKey: preferencesQueryKey, queryFn: fetchPreferences });
}

// One field per call, a genuine partial PATCH -- the server applies only
// the keys present in the body and leaves every omitted toggle untouched
// (see notificationPreferencesRequest's pointer fields in
// household_handlers.go), so a single toggle click never risks touching
// the other three.
function useUpdatePreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      vars: Partial<NotificationPreferences>,
    ): Promise<NotificationPreferences> => {
      const body = await apiFetch<unknown>("/api/v1/notification-preferences", {
        method: "PATCH",
        body: JSON.stringify(vars),
      });
      return notificationPreferencesSchema.parse(body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: preferencesQueryKey });
      queryClient.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

const TOGGLES: { key: keyof NotificationPreferences; label: string }[] = [
  { key: "billReminders", label: "Bill due reminders (3 days before)" },
  { key: "overspendAlerts", label: "Budget over-spend alerts" },
  { key: "retroReminder", label: "Monthly retro reminder" },
  { key: "weeklyDigest", label: "Weekly family digest (Sun 8am)" },
];

export function NotificationsPanel() {
  const me = useMe();
  const preferences = usePreferences();
  const updatePreferences = useUpdatePreferences();
  const isOwner = me.data?.membership.role === "owner";

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 className="mb-4 text-sm font-semibold text-ink">Notifications</h2>

      {preferences.isPending && <p className="text-xs text-muted">Loading…</p>}
      {preferences.isError && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load notification preferences.
        </p>
      )}

      {preferences.isSuccess && (
        <div className="grid grid-cols-1 gap-3.5 text-[13px] sm:grid-cols-2 sm:gap-x-10">
          {TOGGLES.map(({ key, label }) => (
            <div key={key} className="flex items-center justify-between">
              <span className="text-ink">{label}</span>
              <ToggleSwitch
                checked={preferences.data[key]}
                disabled={!isOwner || updatePreferences.isPending}
                onChange={() => updatePreferences.mutate({ [key]: !preferences.data[key] })}
                label={label}
              />
            </div>
          ))}
        </div>
      )}

      {updatePreferences.isError && (
        <p role="alert" className="mt-3 text-[11px] text-danger">
          Something went wrong saving that. Please try again.
        </p>
      )}
    </section>
  );
}
