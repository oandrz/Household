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

// A knock is one tap on a Telegram invite link. `username` is null when
// Telegram sent none -- an @username is optional, and the card says so in
// words rather than rendering an empty "@". `code` is the four digits the
// owner compares with the phone in front of them; it is display-only and no
// request ever carries it back.
const inviteKnockSchema = z.object({
  username: z.string().nullable(),
  code: z.string(),
  knockedAt: z.string(),
});

// GET /household/invites' one row (pending_invite_handlers.go's
// inviteSummaryDTO -- not admin_directory_handlers.go's pendingInviteDTO,
// which is the operator's differently-shaped view). Owner-only, so `email`
// is always the real address.
export const pendingInviteSchema = z.object({
  id: z.string(),
  name: z.string(),
  email: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
  // Both default to the milestone-1 shape, so a server that predates
  // migration 00021 still parses: this is the same rule the /me schema's
  // `features` default follows (auth/schemas.ts), and the reason the two
  // milestones can deploy independently.
  channel: z.string().default("email"),
  knock: inviteKnockSchema.nullish(),
  expiresAt: z.string(),
});
export type PendingInvite = z.infer<typeof pendingInviteSchema>;

export const pendingInvitesSchema = z.array(pendingInviteSchema);

// POST /household/invites/:id/link's body (Task 8) -- reissues a one-time
// t.me link for a Telegram invite whose previous link expired or was never
// tapped.
export const inviteLinkSchema = z.object({
  link: z.string(),
  expiresAt: z.string(),
});
export type InviteLink = z.infer<typeof inviteLinkSchema>;

// POST /household/members/invite's body (Task 12) -- member_handlers.go's
// inviteCreatedDTO. That struct's own comment explains why every field is
// optional: an email invite has no id to report (Create predates this
// response shape), and a zero timestamp would print as a real-looking date
// that is actually a lie, so the profile and email paths answer `{}`.
// Only a Telegram invite carries all three. Reuses inviteLinkSchema's own
// `link`/`expiresAt` shapes rather than inventing a parallel pair --
// inviteLinkSchema itself stays required, since POST .../link always
// returns a real link.
export const inviteCreatedSchema = z.object({
  id: z.string().optional(),
  link: inviteLinkSchema.shape.link.optional(),
  expiresAt: inviteLinkSchema.shape.expiresAt.optional(),
});
export type InviteCreated = z.infer<typeof inviteCreatedSchema>;

// POST /household/invites/:id/admit's body (Task 9) -- the request carries
// no fields at all, since the four digits are compared by eye, not sent
// back. `signInSent` tells the owner whether the new member's sign-in link
// actually went out, distinct from the member having been created.
//
// admittedMemberSchema is its own shape, not memberSchema (above): the
// admit route's admittedMemberDTO is flat -- {id, name, role, capabilities}
// -- while memberSchema nests a full userSchema under `user`. Verified
// against usecase/invite.go and adapter/postgres/invite_repo.go's Admit:
// the name comes from the invite row (`claimed.Name`), not the user record
// it creates, so there is no user object -- email, avatar initial -- to
// nest here.
const admittedMemberSchema = z.object({
  id: z.string(),
  name: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
});

export const admitResultSchema = z.object({
  member: admittedMemberSchema,
  signInSent: z.boolean(),
});
export type AdmitResult = z.infer<typeof admitResultSchema>;

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
