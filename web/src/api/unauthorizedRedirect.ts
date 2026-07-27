// The reaction apiFetch installs (via setUnauthorizedHandler, see client.ts)
// for a 401 outside the pre-auth request paths: clear the query cache and
// send the caller to /sign-in. Pulled out of main.tsx into its own module so
// a test can build one against its own router and QueryClient instances,
// exercised against the real route tree, instead of only against main.tsx's
// app-wide singletons (which main.tsx itself is the only thing that
// constructs).
//
// Fix round 5, Finding 1 (critical): a 401 from a *request* being
// pre-auth-exempt is not the same thing as the *screen making that request*
// being reachable pre-auth. GET /api/v1/auth/me is not, and must not be, in
// client.ts's pre-auth path list -- it's the endpoint whose 401 this handler
// exists to react to (a revoked session in an open tab). But two screens
// call useMe() from a route that is itself reachable with no session at
// all: /sign-in itself (checking "is someone already signed in") and
// /invite/$token (InviteScreen checking "are you signed in as someone
// else"). For a genuinely signed-out visitor -- every invitee, Christine
// from `make seed` included -- that 401 is completely expected and belongs
// to the screen that asked, not to "this tab's session was just revoked".
// Reacting to it there cleared the cache and navigated to /sign-in out from
// under the one screen that has no other path back to itself.
//
// The fix reasons about *where the caller currently is*, not which request
// path 401'd: isOnPublicRoute checks the router's current location at the
// moment the 401 arrives, and the handler is a no-op there. An
// already-authenticated shell route (e.g. /settings) getting a 401 from
// /auth/me is untouched by this and still triggers the real reaction, which
// is the behaviour the finding this handler was built for needs.
import { PUBLIC_ROUTE_PREFIXES } from "../routes/publicRoutes";

interface RedirectableRouter {
  state: { location: { pathname: string } };
  navigate(opts: { to: string; replace?: boolean }): unknown;
}

interface ClearableQueryClient {
  clear(): void;
}

// PUBLIC_ROUTE_PREFIXES (imported above, from routes/publicRoutes.ts) names
// every route whose own component calls useMe() (directly or, for the invite
// screen, indirectly through InviteScreen) while genuinely reachable with no
// session at all.
export function isOnPublicRoute(pathname: string): boolean {
  return PUBLIC_ROUTE_PREFIXES.some((prefix) => pathname.startsWith(prefix));
}

// createUnauthorizedHandler builds the callback main.tsx installs via
// setUnauthorizedHandler. Kept as a factory (not a bare function closing
// over the app's singletons) so a test can construct one against a
// throwaway router and QueryClient.
export function createUnauthorizedHandler(
  router: RedirectableRouter,
  queryClient: ClearableQueryClient,
): () => void {
  return () => {
    if (isOnPublicRoute(router.state.location.pathname)) return;
    queryClient.clear();
    // replace: true, matching RequireAuth's own redirect for the identical
    // event -- otherwise a caller signed out while on "/" gets an extra
    // history entry and Back bounces them straight into another redirect.
    router.navigate({ to: "/sign-in", replace: true });
  };
}
