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
