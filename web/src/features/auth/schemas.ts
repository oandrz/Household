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

// signUpPreviewSchema is GET /auth/sign-up/:token's body. channel is
// "email" | "telegram" -- parsed as a union rather than a bare string so an
// unrecognised value fails loudly here instead of silently rendering the
// wrong screen, the same refuse-what-you-did-not-construct rule
// signup_handlers.go's own switch over Provision.Channel follows server-side.
// For a Telegram sign-up email is present and empty, never absent (the
// handler builds the body with map[string]string specifically so
// `omitempty` can't drop it), so this deliberately does not make email
// optional.
export const signUpPreviewSchema = z.object({
  email: z.string(),
  channel: z.union([z.literal("email"), z.literal("telegram")]),
});
export type SignUpPreview = z.infer<typeof signUpPreviewSchema>;

// currencySchema mirrors currencyDTO (api/internal/adapter/http/currency_handlers.go).
export const currencySchema = z.object({
  code: z.string(),
  // Go marks `symbol` `omitempty`, so it is absent for codes we have no symbol
  // for -- optional here for the same reason spaceDTO.requiredCapability is.
  symbol: z.string().optional(),
  name: z.string(),
});
export const currencyListSchema = z.object({ currencies: z.array(currencySchema) });
export type Currency = z.infer<typeof currencySchema>;
