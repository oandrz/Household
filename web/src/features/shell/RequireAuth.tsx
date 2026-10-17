// A pathless layout route: mounted as the parent of every authenticated
// route, it renders nothing of its own on success beyond <Outlet />, so the
// guard is invisible when it passes.
import { Navigate, Outlet } from "@tanstack/react-router";
import { ApiError } from "../../api/client";
import { useMe } from "../auth/useAuth";

export function RequireAuth() {
  const me = useMe();

  if (me.isPending) {
    return (
      <main className="grid min-h-dvh place-items-center">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  if (me.isError) {
    const status = me.error instanceof ApiError ? me.error.status : undefined;
    if (status === 401) {
      return <Navigate to="/sign-in" replace />;
    }
    // A background refetch that fails keeps the data it already had. That
    // is still the right person, so the app stays: swapping it for this
    // screen would throw away whatever the person was in the middle of.
    if (me.data !== undefined) {
      return <Outlet />;
    }
    // Not "you are not signed in" -- a 500 or a network failure shouldn't be
    // presented as if it were, since bouncing to the sign-in screen would
    // suggest re-entering credentials fixes something that isn't a
    // credentials problem. A household over its request allowance, or a
    // request that ran out of time, is told so in the server's own words:
    // both clear by waiting, which "something went wrong" would hide.
    const code = me.error instanceof ApiError ? me.error.code : undefined;
    const message =
      code === "RATE_LIMITED" || code === "TIMEOUT"
        ? me.error.message
        : "Something went wrong loading your account. Please try again.";
    return (
      <main className="grid min-h-dvh place-items-center p-10 text-center">
        <p className="text-sm text-muted">{message}</p>
      </main>
    );
  }

  return <Outlet />;
}
