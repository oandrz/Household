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
    // Not "you are not signed in" -- a 500 or a network failure shouldn't be
    // presented as if it were, since bouncing to the sign-in screen would
    // suggest re-entering credentials fixes something that isn't a
    // credentials problem.
    return (
      <main className="grid min-h-dvh place-items-center p-10 text-center">
        <p className="text-sm text-muted">
          Something went wrong loading your account. Please try again.
        </p>
      </main>
    );
  }

  return <Outlet />;
}
