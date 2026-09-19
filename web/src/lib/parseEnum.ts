// Narrows a string that came from the DOM (a <select>'s value) to one of a
// known set, keeping `fallback` when it is not in the set. It is CLAUDE.md's
// "fail closed on values you did not construct" rule applied in the browser:
// `event.target.value as Kind` lets any string through, typed as if it were
// valid, all the way into a request body.
export function parseEnum<T extends string>(
  value: string,
  allowed: readonly T[],
  fallback: T,
): T {
  const isAllowed = (candidate: string): candidate is T =>
    (allowed as readonly string[]).includes(candidate);
  return isAllowed(value) ? value : fallback;
}
