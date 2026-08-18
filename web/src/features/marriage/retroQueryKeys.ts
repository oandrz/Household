// The Retros feature's TanStack Query keys, in their own module so both
// useRetros.ts (the history-list screen) and useRetro.ts (one month's
// detail screen) can invalidate each other's cache without importing from
// each other. Before this file existed, useRetro.ts imported
// retroListQueryKey from useRetros.ts (every month-scoped write moves a row
// on the list too), and useRetros.ts could not import retroQueryKey back
// from useRetro.ts without creating a circular import -- so startRetro's own
// success handler could only invalidate the list, never the specific month
// it had just created. That gap was real, not cosmetic: useRetro(month)
// caches a 404 for "no retro yet" the same as any other response, and a tab
// that had already rendered the startable month's detail screen (or simply
// has it mounted, e.g. via a stale link) kept serving that cached miss
// straight through the click that created the retro. Both hooks import
// their keys from here now, so startRetro can invalidate both.
export function retroListQueryKey() {
  return ["retros"] as const;
}

export function retroQueryKey(month: string) {
  return ["retro", month] as const;
}
