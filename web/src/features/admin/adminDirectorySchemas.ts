// Zod mirrors of the DTOs in api/internal/adapter/http/
// admin_directory_handlers.go -- the adminSchemas.ts convention: follow the
// backend's own structs, not a guess at the shape. Every object is .strict()
// so a key the backend did not promise (a money field, say) fails the parse
// rather than reaching a screen the spec says never shows money.
import { z } from "zod";

export const adminMemberMatchSchema = z
  .object({ memberName: z.string(), memberEmail: z.string().nullable() })
  .strict();

export const adminHouseholdListingSchema = z
  .object({
    id: z.string(),
    name: z.string(),
    familyName: z.string(),
    memberCount: z.number().int(),
    createdAt: z.string(),
    lastActiveAt: z.string().nullable(),
    primaryCurrency: z.string(),
    // Set only when a member, not the household, matched the search.
    match: adminMemberMatchSchema.nullable(),
  })
  .strict();
export type AdminHouseholdListing = z.infer<typeof adminHouseholdListingSchema>;

export const adminDirectoryMetricsSchema = z
  .object({
    households: z.number().int(),
    activeHouseholds7d: z.number().int(),
    signups30d: z
      .object({ requested: z.number().int(), completed: z.number().int() })
      .strict(),
    pendingInvites: z.number().int(),
  })
  .strict();

export const adminHouseholdsResponseSchema = z
  .object({
    metrics: adminDirectoryMetricsSchema,
    households: z.array(adminHouseholdListingSchema),
    truncated: z.boolean(),
  })
  .strict();
export type AdminHouseholdsResponse = z.infer<
  typeof adminHouseholdsResponseSchema
>;

export const adminHouseholdMemberSchema = z
  .object({
    userId: z.string(),
    name: z.string(),
    email: z.string().nullable(),
    // The backend's channelString fails closed on anything else; so does
    // this enum, on the client's side of the same boundary.
    channel: z.enum(["email", "telegram"]),
    role: z.enum(["owner", "limited"]),
    capabilities: z.array(z.string()),
    lastActiveAt: z.string().nullable(),
  })
  .strict();
export type AdminHouseholdMember = z.infer<typeof adminHouseholdMemberSchema>;

export const adminPendingInviteSchema = z
  .object({
    name: z.string(),
    email: z.string(),
    role: z.enum(["owner", "limited"]),
    invitedByName: z.string(),
    expiresAt: z.string(),
  })
  .strict();

export const adminHouseholdPageSchema = z
  .object({
    household: z
      .object({
        id: z.string(),
        name: z.string(),
        familyName: z.string(),
        createdAt: z.string(),
        primaryCurrency: z.string(),
      })
      .strict(),
    members: z.array(adminHouseholdMemberSchema),
    pendingInvites: z.array(adminPendingInviteSchema),
    lockout: z.object({ lockedUntil: z.string() }).strict().nullable(),
  })
  .strict();
export type AdminHouseholdPage = z.infer<typeof adminHouseholdPageSchema>;
