// Zod mirrors of the DTOs in api/internal/adapter/http/
// admin_browse_handlers.go -- the adminOutboxSchemas.ts convention: follow
// the backend's own structs, not a guess at the shape.
//
// Every object is .strict(), and here that is load-bearing rather than tidy.
// This is the one screen in Hearth that renders arbitrary table contents, so
// a field the frontend did not expect must fail the parse rather than reach
// a page that will happily print whatever it is handed.
import { z } from "zod";

export const adminDatabaseColumnSchema = z
  .object({
    name: z.string(),
    dataType: z.string(),
    // True when the value is withheld rather than absent. The screen must
    // show the difference; see the legend in AdminDatabasePage.
    redacted: z.boolean(),
  })
  .strict();
export type AdminDatabaseColumn = z.infer<typeof adminDatabaseColumnSchema>;

export const adminDatabaseTableSchema = z
  .object({
    name: z.string(),
    rowCount: z.number().int(),
    columns: z.array(adminDatabaseColumnSchema),
  })
  .strict();
export type AdminDatabaseTable = z.infer<typeof adminDatabaseTableSchema>;

export const adminDatabaseTablesSchema = z
  .object({ tables: z.array(adminDatabaseTableSchema) })
  .strict();
export type AdminDatabaseTables = z.infer<typeof adminDatabaseTablesSchema>;

export const adminDatabaseRowsSchema = z
  .object({
    table: z.string(),
    columns: z.array(adminDatabaseColumnSchema),
    // Column-ordered text, parallel to columns. Never objects: a table may
    // carry two columns whose names collide as JSON keys.
    rows: z.array(z.array(z.string())),
    total: z.number().int(),
    limit: z.number().int(),
    offset: z.number().int(),
  })
  .strict();
export type AdminDatabaseRows = z.infer<typeof adminDatabaseRowsSchema>;
