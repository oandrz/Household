// Turns any mutation or query error into copy a screen can show. Not
// auth-specific: every feature's onError uses it, so it lives beside
// apiFetch and ApiError rather than inside one feature.
import { ApiError } from "./client";

// A mutation's onError handler receives `unknown`, not just ApiError -- a
// network rejection or a schema-parse failure inside the mutationFn reaches
// it too. Filtering those out and rendering nothing (as an earlier version
// of this screen did) leaves the caller with no visible feedback at all, so
// every caller of this must always get a string back: the server's own
// message when it's an ApiError, and a generic fallback otherwise.
export function apiErrorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}
