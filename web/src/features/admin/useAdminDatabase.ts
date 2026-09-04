// Query hooks over the database browse routes (api/internal/adapter/http/
// admin_browse_handlers.go). Same shape as useAdminOutbox.ts, same two rules:
// refetchOnWindowFocus is off because every request under /admin is an audit
// row -- and on this screen an audit row is the record that someone read a
// household's money -- and a lapsed grant is not handled here, it is routed
// to the one AdminGate AdminShell owns.
//
// Retries are off globally (main.tsx sets retry: false) and neither hook sets
// its own. A retried 503 would be four audit rows per failed page load.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  adminDatabaseRowsSchema,
  adminDatabaseTablesSchema,
  type AdminDatabaseRows,
  type AdminDatabaseTables,
} from "./adminDatabaseSchemas";

// Re-exported so a reader looking for the limits finds them beside the hooks
// that use them. They are declared in browseLimits.ts because router.tsx
// needs them for validateSearch and may never statically import a hook file
// -- adminBundleSplit.test.ts walks main.tsx's import graph and fails if any
// admin hook becomes reachable from it. directoryLimits.ts exists for exactly
// this reason.
export { BROWSE_DEFAULT_LIMIT, BROWSE_MAX_LIMIT } from "./browseLimits";

export function adminDatabaseTablesPath(): string {
  return "/api/v1/admin/db/tables";
}

export function adminDatabaseRowsPath(
  table: string,
  limit: number,
  offset: number,
): string {
  return `/api/v1/admin/db/tables/${encodeURIComponent(table)}?limit=${String(limit)}&offset=${String(offset)}`;
}

export function adminDatabaseTablesKey() {
  return ["admin", "database", "tables"] as const;
}

export function adminDatabaseRowsKey(
  table: string,
  limit: number,
  offset: number,
) {
  return ["admin", "database", "rows", { table, limit, offset }] as const;
}

async function fetchTables(): Promise<AdminDatabaseTables> {
  const body = await apiFetch<unknown>(adminDatabaseTablesPath());
  return adminDatabaseTablesSchema.parse(body);
}

async function fetchRows(
  table: string,
  limit: number,
  offset: number,
): Promise<AdminDatabaseRows> {
  const body = await apiFetch<unknown>(
    adminDatabaseRowsPath(table, limit, offset),
  );
  return adminDatabaseRowsSchema.parse(body);
}

export function useAdminDatabaseTables() {
  return useQuery({
    queryKey: adminDatabaseTablesKey(),
    queryFn: fetchTables,
    refetchOnWindowFocus: false,
  });
}

export function useAdminDatabaseRows(
  table: string,
  limit: number,
  offset: number,
) {
  return useQuery({
    queryKey: adminDatabaseRowsKey(table, limit, offset),
    queryFn: () => fetchRows(table, limit, offset),
    refetchOnWindowFocus: false,
  });
}
