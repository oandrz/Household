// Zod schemas for the identity endpoints. These mirror the DTOs declared in
// api/internal/adapter/http/auth_handlers.go (meResponseBody and its nested
// DTOs) and invite_handlers.go (invitePreviewResponse).
//
// Schemas are deliberately tolerant -- no .strict(), and the one field Go
// marks `omitempty` (spaceDTO.requiredCapability) is optional here too -- so
// a valid backend response can never fail to parse just because the
// frontend's model of it is slightly narrower than the wire shape.
import { z } from "zod";

export const userSchema = z.object({
  id: z.string(),
  email: z.string(),
  displayName: z.string(),
  avatarInitial: z.string(),
});
export type User = z.infer<typeof userSchema>;

export const householdSchema = z.object({
  id: z.string(),
  name: z.string(),
  familyName: z.string(),
  primaryCurrency: z.string(),
  showSecondaryCurrency: z.boolean(),
  secondaryCurrency: z.string(),
  fxRateMode: z.string(),
});
export type Household = z.infer<typeof householdSchema>;

export const membershipSchema = z.object({
  id: z.string(),
  householdId: z.string(),
  userId: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
});
export type Membership = z.infer<typeof membershipSchema>;

export const spaceSchema = z.object({
  id: z.string(),
  key: z.string(),
  name: z.string(),
  visibility: z.string(),
  position: z.number(),
  isBuiltin: z.boolean(),
  requiredCapability: z.string().optional(),
});
export type Space = z.infer<typeof spaceSchema>;

// meQuerySchema is GET /auth/me's body -- also what a successful sign-in,
// magic-link consumption and invite acceptance answer with (see
// completeSignIn in auth_handlers.go).
export const meQuerySchema = z.object({
  user: userSchema,
  household: householdSchema,
  membership: membershipSchema,
  capabilities: z.array(z.string()),
  spaces: z.array(spaceSchema),
});
export type Me = z.infer<typeof meQuerySchema>;

export const signInRequestSchema = z.object({
  email: z.string(),
  password: z.string(),
});
export type SignInRequest = z.infer<typeof signInRequestSchema>;

export const magicLinkRequestSchema = z.object({
  email: z.string(),
});
export type MagicLinkRequest = z.infer<typeof magicLinkRequestSchema>;

// invitePreviewSchema is GET /invites/:token's body.
export const invitePreviewSchema = z.object({
  householdName: z.string(),
  inviterName: z.string(),
  name: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
});
export type InvitePreview = z.infer<typeof invitePreviewSchema>;

export const acceptInviteRequestSchema = z.object({
  password: z.string(),
  displayName: z.string(),
});
export type AcceptInviteRequest = z.infer<typeof acceptInviteRequestSchema>;
