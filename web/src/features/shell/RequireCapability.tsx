// Client-side gating is presentation only, per the identity spec: the server
// enforces every capability rule independently on the endpoints themselves.
// This exists so a direct link or a stale bookmark to a space a member has
// lost access to (or never had) bounces to the Overview instead of rendering
// content the sidebar wouldn't have offered -- it is not the source of
// truth, and its absence would not be a security hole, only a worse UI for
// an already-rejected request.
//
// Only used nested under RequireAuth (an ancestor route), so `useMe()` here
// reads the same already-resolved ['me'] cache entry -- there is nothing new
// to await.
import { Navigate, Outlet } from "@tanstack/react-router";
import { useMe } from "../auth/useAuth";

export function RequireCapability({ cap }: { cap: string }) {
  const me = useMe();

  if (!me.data) return null;
  if (!me.data.capabilities.includes(cap)) {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
