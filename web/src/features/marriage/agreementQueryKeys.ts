// The Agreements feature's TanStack Query key, in its own module for the
// reason visionQueryKeys.ts:1-4 records: AgreementsPage and the Retros page's
// To-discuss block read the same document, and neither should import the
// other just to invalidate a cache.
//
// No parameter. Every other query key in this codebase takes the thing it
// addresses (a year, a month, a goal id); this one addresses "the household's
// agreements", and the session already fixes the household -- a householdId
// parameter would be a value the browser holds and the server ignores.
export function agreementsQueryKey() {
  return ["agreements"] as const;
}
