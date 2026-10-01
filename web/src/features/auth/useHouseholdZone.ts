// The time zone the signed-in household keeps its calendar in, for
// lib/householdDate.ts: `todayIn(useHouseholdZone())`.
import { useMe } from "./useAuth";

// What a component gets before GET /auth/me has answered. Inside the app that
// does not happen: RequireAuth renders nothing else until the bundle has
// loaded, so every screen behind it already has the real zone. It does
// happen in a component test that mounts on a cold cache, for one render.
//
// UTC rather than the browser's zone, so that the gap, if it ever shows, shows
// the same value on every device. Don't keep a date worked out from this in
// state at mount: derive it during render, so it becomes right the moment the
// bundle arrives.
const ZONE_BEFORE_ME_LOADS = "UTC";

export function useHouseholdZone(): string {
  const me = useMe();
  return me.data?.household.timezone ?? ZONE_BEFORE_ME_LOADS;
}
