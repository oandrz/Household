// Zod schemas for the Settings screen's own endpoints. These mirror the DTOs
// in api/internal/adapter/http/member_handlers.go and household_handlers.go,
// the same way features/auth/schemas.ts mirrors auth_handlers.go /
// invite_handlers.go. Deliberately tolerant (no .strict()), matching that
// file's own convention.
import { z } from "zod";
import { userSchema } from "../auth/schemas";

// GET /household/members' one row. userDTO.Email has no `omitempty` on the
// wire, so it is always present -- either the real address (an owner
// caller) or "" (a non-owner caller, or a genuinely credential-less child;
// see MembersPanel.tsx for why this screen renders neither).
export const memberSchema = z.object({
  id: z.string(),
  user: userSchema,
  role: z.string(),
  capabilities: z.array(z.string()),
});
export type MemberView = z.infer<typeof memberSchema>;

export const membersListSchema = z.array(memberSchema);

// PATCH /household/members/:id's success body. `warning` is present only
// when usecase.ErrSessionRevocationFailed fired -- the mutation still
// committed (the response is still 200 with the normal fields), but the
// member's other sessions may still be live. Optional, not a bare string
// default, so its absence is distinguishable from an empty one.
export const updateMemberResponseSchema = z.object({
  id: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
  warning: z.string().optional(),
});
export type UpdateMemberResponse = z.infer<typeof updateMemberResponseSchema>;

export const notificationPreferencesSchema = z.object({
  billReminders: z.boolean(),
  overspendAlerts: z.boolean(),
  retroReminder: z.boolean(),
  weeklyDigest: z.boolean(),
});
export type NotificationPreferences = z.infer<typeof notificationPreferencesSchema>;

// Mirrors telegram_handlers.go's telegramBindingResponse -- what GET, POST
// .../confirm and DELETE /auth/telegram all answer with.
export const telegramBindingSchema = z.object({
  connected: z.boolean(),
  chatUsername: z.string().optional(),
  linkedAt: z.string().optional(),
});
export type TelegramBinding = z.infer<typeof telegramBindingSchema>;

// Mirrors telegramLinkStartResponse -- POST /auth/telegram/link's body.
export const telegramLinkStartSchema = z.object({
  id: z.string(),
  url: z.string(),
  expiresAt: z.string(),
});
export type TelegramLinkStart = z.infer<typeof telegramLinkStartSchema>;

// status is a closed set the server owns. Parsed as an enum rather than a
// string so an unknown value fails loudly here instead of rendering a panel
// with no branch taken.
export const telegramLinkStatusSchema = z.object({
  status: z.enum(["waiting", "pending", "connected", "refused", "expired"]),
  chatUsername: z.string().optional(),
  reason: z.string().optional(),
});
export type TelegramLinkStatus = z.infer<typeof telegramLinkStatusSchema>;
