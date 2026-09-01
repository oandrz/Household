// Zod mirrors of the DTOs in api/internal/adapter/http/admin_handlers.go
// (flagOverrideDTO, flagDTO, flagsResponse) -- the goalSchemas.ts convention:
// follow the backend's own structs, not a guess at the shape.
import { z } from "zod";

// adminFlagOverrideSchema mirrors flagOverrideDTO -- one household's
// departure from the global value, or from the compile-time default if no
// global value is set either.
export const adminFlagOverrideSchema = z.object({
  householdId: z.string(),
  householdName: z.string(),
  enabled: z.boolean(),
});

// adminFlagSchema mirrors flagDTO. globalSet/globalEnabled are two separate
// booleans, not one nullable one, because "no global opinion" and
// "explicitly off" are different states the screen has to tell apart --
// handleClearHouseholdFlag's own comment makes the identical point about
// household overrides. orphaned marks an override row naming a flag this
// build's registry no longer defines.
export const adminFlagSchema = z.object({
  key: z.string(),
  description: z.string(),
  default: z.boolean(),
  globalSet: z.boolean(),
  globalEnabled: z.boolean(),
  effective: z.boolean(),
  orphaned: z.boolean(),
  overrides: z.array(adminFlagOverrideSchema),
});
export type AdminFlag = z.infer<typeof adminFlagSchema>;

// adminFlagsResponseSchema is the {"flags": [...]} shape every flags route
// answers -- the GET, and every PUT/DELETE, which all echo the refreshed
// list (see admin_handlers.go's own comments on why: "the screen never has
// to guess what the write did to the effective values").
export const adminFlagsResponseSchema = z.object({
  flags: z.array(adminFlagSchema),
});
export type AdminFlagsResponse = z.infer<typeof adminFlagsResponseSchema>;
