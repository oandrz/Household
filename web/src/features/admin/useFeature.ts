// useFeature answers whether a feature flag is on for the signed-in caller.
//
// It reads the already-cached /auth/me bundle rather than fetching anything:
// the server resolved these once, per request, and a second source of truth
// in the client would drift from the guard that actually enforces.
//
// This is not enforcement. requireFeature on the server is; this only avoids
// showing a door that opens onto a 404. A key the server did not send (a
// typo, or an older server this build doesn't match) must resolve to false,
// the same fail-closed rule the server's own FlagSet.Enabled follows for any
// flag its build does not define -- a typo in a caller must close a door,
// never open one.
import { useMe } from "../auth/useAuth";

export function useFeature(key: string): boolean {
  const { data } = useMe();
  return data?.features?.[key] === true;
}
