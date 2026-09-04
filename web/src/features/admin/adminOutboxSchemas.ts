// Zod mirrors of the DTOs in api/internal/adapter/http/
// admin_outbox_handlers.go -- the adminDirectorySchemas.ts convention:
// follow the backend's own structs, not a guess at the shape.
//
// Every object is .strict(), and here that is load-bearing rather than
// tidy: the list must never carry body text (a Hearth email is short enough
// that a snippet can contain the whole link), and a message must never carry
// an HTML part. A backend change that added either would fail the parse
// instead of quietly reaching a screen.
import { z } from "zod";

export const adminMailSummarySchema = z
  .object({
    id: z.string(),
    to: z.string(),
    subject: z.string(),
    sentAt: z.string(),
  })
  .strict();
export type AdminMailSummary = z.infer<typeof adminMailSummarySchema>;

export const adminMailListSchema = z
  .object({
    messages: z.array(adminMailSummarySchema),
    total: z.number().int(),
    truncated: z.boolean(),
  })
  .strict();
export type AdminMailList = z.infer<typeof adminMailListSchema>;

export const adminMailMessageSchema = z
  .object({
    id: z.string(),
    to: z.string(),
    subject: z.string(),
    sentAt: z.string(),
    links: z.array(z.string()),
    text: z.string(),
  })
  .strict();
export type AdminMailMessage = z.infer<typeof adminMailMessageSchema>;
