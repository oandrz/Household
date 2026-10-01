// The Vision feature's TanStack Query key, in its own module for the reason
// retroQueryKeys.ts records: Overview's VisionCard reads the same year's
// vision as VisionPage does, and neither should import from the other just to
// invalidate a cache.
export function visionQueryKey(year: number) {
  return ["vision", year] as const;
}
