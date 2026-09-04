// Query hooks over the outbox routes (api/internal/adapter/http/
// admin_outbox_handlers.go). Same shape as useAdminDirectory.ts, same two
// rules: refetchOnWindowFocus is off because every request under /admin is an
// audit row, and a lapsed grant is not handled here -- it is routed to the
// one AdminGate AdminShell owns (useCloseSurfaceOnReauth).
//
// Retries are off too, and already off globally: main.tsx sets retry: false
// on every query. Neither hook below sets its own, and neither should -- a
// retried 503 would be four audit rows per failed page load and several
// seconds of spinner before the unavailability copy ever appeared.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  adminMailListSchema,
  adminMailMessageSchema,
  type AdminMailList,
  type AdminMailMessage,
} from "./adminOutboxSchemas";

// The service's own clamps, mirrored so the page can say "showing the newest
// 50" without a round trip. They must match usecase/admin_outbox.go.
export const OUTBOX_DEFAULT_LIMIT = 50;
export const OUTBOX_MAX_LIMIT = 200;

export function adminMailPath(limit: number): string {
  return `/api/v1/admin/mail?limit=${String(limit)}`;
}

export function adminMailKey(limit: number) {
  return ["admin", "mail", { limit }] as const;
}

export function adminMailMessageKey(messageId: string) {
  return ["admin", "mail", "message", messageId] as const;
}

async function fetchAdminMail(limit: number): Promise<AdminMailList> {
  const body = await apiFetch<unknown>(adminMailPath(limit));
  return adminMailListSchema.parse(body);
}

async function fetchAdminMailMessage(
  messageId: string,
): Promise<AdminMailMessage> {
  const body = await apiFetch<unknown>(
    `/api/v1/admin/mail/${encodeURIComponent(messageId)}`,
  );
  return adminMailMessageSchema.parse(body);
}

export function useAdminMail(limit: number) {
  return useQuery({
    queryKey: adminMailKey(limit),
    queryFn: () => fetchAdminMail(limit),
    refetchOnWindowFocus: false,
  });
}

export function useAdminMailMessage(messageId: string) {
  return useQuery({
    queryKey: adminMailMessageKey(messageId),
    queryFn: () => fetchAdminMailMessage(messageId),
    refetchOnWindowFocus: false,
  });
}
