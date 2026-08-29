// Zod mirrors of the DTOs in api/internal/adapter/http/vision_handlers.go
// (measureDTO, pillarDTO, milestoneDTO, visionDTO, visionResponse). These
// follow the backend's own structs rather than the design doc, the same
// convention retroSchemas.ts and goalSchemas.ts already use -- the backend's
// comments are what say which fields can be meaningless and why.
import { z } from "zod";

// kind mirrors domain.MeasureKind. "broken" is a real state the server can
// send: measureDTO's own comment says a broken link means a measure whose
// linked goal was deleted, rendering with no figure at all. Parsing it as
// its own enum member, rather than falling back to "typed" on anything
// unrecognised, is what stops a broken measure rendering as a confident
// "0 of 0" -- untrue about the household. Anything this schema doesn't
// recognise (a genuine wire drift, or a fourth kind added server-side and
// forgotten here) must fail the parse, not silently become "typed".
export const measureKindSchema = z.enum(["typed", "linked", "broken"]);

// visionMeasureSchema mirrors measureDTO. hasFigure is the field the screen
// actually branches on (measureDTO's own comment): when it is false,
// current, target and percent are all 0 and mean nothing -- they are plain
// ints rather than nullable because hasFigure already carries the "there is
// no figure" state, and this schema does not invent a second way to say the
// same thing. goalId/goalName are "" unless the measure is linked and
// (for goalName) the goal still resolves -- required strings, not optional,
// because toVisionDTO always sends them, empty or not.
export const visionMeasureSchema = z.object({
  label: z.string(),
  kind: measureKindSchema,
  hasFigure: z.boolean(),
  current: z.number().int(),
  target: z.number().int(),
  percent: z.number().int(),
  met: z.boolean(),
  goalId: z.string(),
  goalName: z.string(),
});
export type VisionMeasure = z.infer<typeof visionMeasureSchema>;

// visionPillarSchema mirrors pillarDTO. measures is always an array, never
// null, even when empty -- toVisionDTO builds it with make(..., 0, ...), so
// the wire always carries "[]" and this schema requires the array rather
// than defaulting a missing key.
export const visionPillarSchema = z.object({
  name: z.string(),
  description: z.string(),
  measures: z.array(visionMeasureSchema),
});
export type VisionPillar = z.infer<typeof visionPillarSchema>;

// visionMilestoneSchema mirrors milestoneDTO -- one longer-horizon entry,
// carrying its own year rather than inheriting the vision's.
export const visionMilestoneSchema = z.object({
  year: z.number().int(),
  title: z.string(),
  note: z.string(),
});
export type VisionMilestone = z.infer<typeof visionMilestoneSchema>;

// visionSchema mirrors visionDTO -- one household-year. version 0 means the
// year has no vision yet (visionDTO's own comment); it is what tells a save
// it is a create rather than a blind overwrite, which is why useVision.ts
// always sends back the version it loaded and never one a caller supplies.
// pillars and milestones are always arrays, never null, for the same reason
// measures is on visionPillarSchema above.
export const visionSchema = z.object({
  year: z.number().int(),
  theme: z.string(),
  description: z.string(),
  version: z.number().int(),
  pillars: z.array(visionPillarSchema),
  milestones: z.array(visionMilestoneSchema),
});
export type Vision = z.infer<typeof visionSchema>;

// visionResponseSchema mirrors visionResponse -- the whole body of both
// GET /marriage/vision and PUT /marriage/vision/{year}: the vision alone,
// wrapped.
export const visionResponseSchema = z.object({ vision: visionSchema });
export type VisionResponse = z.infer<typeof visionResponseSchema>;
