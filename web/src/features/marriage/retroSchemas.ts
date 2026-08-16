// Zod mirrors of the DTOs in api/internal/adapter/http/retro_handlers.go
// (retroDTO, retroActionDTO, retroSummaryDTO, moodPointDTO, retrosResponse,
// retroResponse, retroWriteResponse, retroActionResponse,
// retroActionTickResponse). These follow the backend's own structs rather
// than the design doc, the same convention goalSchemas.ts/budgetSchemas.ts
// already use -- the backend's comments are what say which fields can be
// null and why. Field names and sample values below are taken from
// .superpowers/sdd/2026-08-16-hearth-retros/task-7-report.md and
// task-8-report.md, both captured from a real seeded retro read back over
// HTTP rather than hand-transcribed from the Go structs.
import { z } from "zod";

// moodSchema mirrors every *int mood field on the wire (retroDTO.Mood,
// retroSummaryDTO.Mood, moodPointDTO.Mood): 1-5 when someone has picked an
// emoji, null when nobody has -- retroDTO's own comment. A literal 0 is not
// a valid mood on the Go side either (saveRetroRequest's own comment: "0 is
// not a valid mood"), so this schema refuses one rather than letting a
// buggy server's 0 render as if it meant something -- the frontend's own
// fail-closed boundary, per the brief.
const moodSchema = z.number().int().min(1).max(5).nullable();

// moodPointSchema mirrors moodPointDTO -- one point on the twelve-month mood
// chart. Mood is null for a gap, never 0 (moodPointDTO's own comment: "null
// is a gap, never 0").
export const moodPointSchema = z.object({
  month: z.string(),
  mood: moodSchema,
});
export type MoodPoint = z.infer<typeof moodPointSchema>;

// retroActionSchema mirrors retroActionDTO. carriedFrom is "" rather than
// absent when the action was not carried, and assigneeMembershipIds is []
// rather than absent when nobody is assigned yet -- both structurally
// guaranteed by toRetroActionDTO (retro_handlers.go: "assignees := ...; if
// assignees == nil { assignees = []string{} }"), so this schema requires
// them rather than defaulting a missing key. A response that omits either
// is a wire drift this parse should catch, not paper over.
export const retroActionSchema = z.object({
  id: z.string(),
  body: z.string(),
  doneAt: z.string().nullable(),
  carriedFrom: z.string(),
  assigneeMembershipIds: z.array(z.string()),
});
export type RetroAction = z.infer<typeof retroActionSchema>;

// retroSchema mirrors retroDTO -- one month's retro plus the actions it
// owns. completedAt is nullable, not optional: a draft retro still answers
// "completedAt": null, never an absent key (retroDTO's own comment: "null
// means draft").
export const retroSchema = z.object({
  id: z.string(),
  month: z.string(), // "2026-07"
  mood: moodSchema,
  wentWell: z.string(),
  wasHard: z.string(),
  notes: z.string(),
  completedAt: z.string().nullable(),
  version: z.number().int(),
  actions: z.array(retroActionSchema),
});
export type Retro = z.infer<typeof retroSchema>;

// retroSummarySchema mirrors retroSummaryDTO -- one row of the Retros
// history list. quote is "" rather than absent when the retro has no notes
// worth a first sentence yet (RetroService.List's own derivation can
// legitimately be empty) -- required here for the same "no defaulting a
// missing key" reason assigneeMembershipIds is required above.
export const retroSummarySchema = z.object({
  id: z.string(),
  month: z.string(),
  mood: moodSchema,
  actionCount: z.number().int(),
  quote: z.string(),
  finished: z.boolean(),
});
export type RetroSummary = z.infer<typeof retroSummarySchema>;

// retrosResponseSchema mirrors retrosResponse, GET /retros' whole body.
export const retrosResponseSchema = z.object({
  retros: z.array(retroSummarySchema),
  mood: z.array(moodPointSchema),
  doneCount: z.number().int(),
  since: z.string().nullable(), // "2025-08", or null
  startMonth: z.string().nullable(), // null when both candidate months exist
});
export type RetrosResponse = z.infer<typeof retrosResponseSchema>;

// retroDetailResponseSchema mirrors retroResponse, GET /retros/{month}'s
// whole body. carryOver is a top-level sibling of retro, not nested inside
// it -- retroResponse's own comment: a carried-over action belongs to LAST
// month's retro, not this one.
export const retroDetailResponseSchema = z.object({
  retro: retroSchema,
  carryOver: z.array(retroActionSchema),
});
export type RetroDetailResponse = z.infer<typeof retroDetailResponseSchema>;

// retroWriteResponseSchema mirrors retroWriteResponse -- what POST /retros,
// PATCH /retros/{month} and POST /retros/{month}/complete all answer: the
// retro alone, no carryOver (retroWriteResponse's own comment: that field is
// a detail-*screen* concept, not part of the retro resource these three
// writes return).
export const retroWriteResponseSchema = z.object({
  retro: retroSchema,
});

// retroActionResponseSchema mirrors retroActionResponse, POST
// /retros/{month}/actions' whole body: the created action alone, the same
// "a created sub-resource returns itself" shape goal_handlers.go's
// contributionResponse already uses.
export const retroActionResponseSchema = z.object({
  action: retroActionSchema,
});

// retroActionTickResponseSchema mirrors retroActionTickResponse, PATCH
// /retros/{month}/actions/{id}'s whole body -- deliberately NOT
// retroActionSchema. The backend's own comment: SetDone returns no record,
// so this is a genuinely narrower, distinct shape (id + doneAt only), not a
// subset of retroActionSchema that happens to omit some keys. Parsing it
// through its own schema, rather than retroActionSchema with the extra
// fields left undefined, is what stops a caller from ever building a fake
// full action out of this response.
export const retroActionTickResponseSchema = z.object({
  id: z.string(),
  doneAt: z.string().nullable(),
});
