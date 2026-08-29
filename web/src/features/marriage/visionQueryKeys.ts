// The Vision feature's TanStack Query key, in its own module for the reason
// retroQueryKeys.ts records: Overview's VisionCard reads the same year's
// vision as VisionPage does, and neither should import from the other just to
// invalidate a cache.
export function visionQueryKey(year: number) {
  return ["vision", year] as const;
}

// currentVisionYear() is the client's own choice of which year to ask for --
// on VisionPage before the household has picked a different one, and on
// Overview's VisionCard, which has no year picker at all. It is local
// calendar time (money/month.ts's own reasoning: never toISOString(), which
// can read back yesterday's or tomorrow's date for a household west or east
// of UTC), not the source of truth for "the current vision year" -- that stays
// the server's own Clock (VisionService.CurrentYear), which GET
// /marriage/vision falls back to whenever a caller sends no ?year at all.
export function currentVisionYear(): number {
  return new Date().getFullYear();
}
