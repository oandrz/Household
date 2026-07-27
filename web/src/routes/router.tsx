// The application's route tree, built with TanStack Router's code-based API
// (createRootRoute/createRoute/.addChildren) rather than file-based
// generation -- no @tanstack/router-plugin is configured in vite.config.ts,
// so there is no generated route tree to import instead.
//
// Shape:
//   /sign-in, /sign-in/magic, /invite/$token,
//   /sign-up, /sign-up/$token                       -- public, pre-auth (see routes/publicRoutes.ts)
//   RequireAuth (pathless)                          -- redirects to /sign-in on a 401
//     AppShell (pathless)                           -- sidebar + outlet
//       /            Overview      (slice 5 placeholder)
//       /money, /money/$    RequireCapability("money")    -> Money      (slice 2 placeholder)
//       /marriage, /marriage/$ RequireCapability("marriage") -> Marriage (slice 3 placeholder)
//       /family/calendar                            -- Family (slice 4 placeholder); unconditional,
//                                                       per domain.BuiltinSpaces Family carries no
//                                                       required capability
//       /settings                                   -- slice 1; the real Settings screen (Task 20)
import {
  Navigate,
  createRootRoute,
  createRoute,
  createRouter,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import { InviteScreen } from "../features/auth/InviteScreen";
import { MagicLinkConsumeScreen } from "../features/auth/MagicLinkConsumeScreen";
import { SignInScreen } from "../features/auth/SignInScreen";
import { SignUpScreen } from "../features/auth/SignUpScreen";
import { SignUpCompleteScreen } from "../features/auth/SignUpCompleteScreen";
import { useMe } from "../features/auth/useAuth";
import { PlaceholderPage } from "../features/placeholder/PlaceholderPage";
import { SettingsPage } from "../features/settings/SettingsPage";
import { AppShell } from "../features/shell/AppShell";
import { RequireAuth } from "../features/shell/RequireAuth";
import { RequireCapability } from "../features/shell/RequireCapability";

const rootRoute = createRootRoute({
  notFoundComponent: () => (
    <main className="grid min-h-screen place-items-center p-10 text-center">
      <p className="text-sm text-muted">Page not found.</p>
    </main>
  ),
});

// SignInScreen (Task 18) and InviteScreen (below) both invalidate ['me'] on
// success but neither one navigates -- they were built before this route
// tree existed. Rather than teach two pre-auth screens about the router,
// each of their routes carries its own "already signed in -> enter the app"
// redirect: once the invalidated ['me'] query resolves successfully, this
// wrapper takes over and leaves.
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sign-in",
  component: function SignInRouteComponent() {
    const me = useMe();
    if (me.isSuccess) return <Navigate to="/" replace />;
    return <SignInScreen />;
  },
});

const signInMagicRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sign-in/magic",
  validateSearch: (search: Record<string, unknown>): { token: string } => ({
    token: typeof search.token === "string" ? search.token : "",
  }),
  // Not a named, module-scope function: eslint-plugin-react-refresh's
  // only-export-components rule flags a file that mixes a top-level
  // component declaration with router.tsx's other exports (the route
  // objects, `router` itself), even when the component isn't itself
  // exported. An inline function expression passed straight into `component`
  // sidesteps that without moving two one-line wrappers into their own
  // files.
  component: function SignInMagicRouteComponent() {
    const { token } = useSearch({ from: "/sign-in/magic" });
    return <MagicLinkConsumeScreen token={token} />;
  },
});

const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/invite/$token",
  // Deliberately does not redirect an already-signed-in caller away, unlike
  // /sign-in below -- Task 18's App.tsx made this call explicitly ("it must
  // render before there is any session to check") because an invite link is
  // often opened on a shared device that's already signed in as someone
  // else (a family iPad, a shared laptop); bouncing that visitor straight to
  // the dashboard with no explanation and no way to see the invite would be
  // worse than showing it. InviteScreen itself navigates to "/" once its own
  // acceptance succeeds (see InviteScreen.tsx), which is the only case that
  // actually needs "enter the app from here."
  component: function InviteRouteComponent() {
    const { token } = useParams({ from: "/invite/$token" });
    return <InviteScreen token={token} />;
  },
});

const signUpRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sign-up",
  component: function SignUpRouteComponent() {
    const me = useMe();
    // Same "already signed in -> enter the app" wrapper the sign-in route
    // uses: someone with a live session has no use for a create-household
    // form.
    if (me.isSuccess) return <Navigate to="/" replace />;
    return <SignUpScreen />;
  },
});

const signUpCompleteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sign-up/$token",
  // Deliberately does NOT bounce an already-signed-in caller, for the same
  // reason inviteRoute does not: the link is often opened on a shared device
  // that is already signed in as someone else, and dropping that visitor on
  // the dashboard with no explanation would be worse than showing the form.
  component: function SignUpCompleteRouteComponent() {
    const { token } = useParams({ from: "/sign-up/$token" });
    return <SignUpCompleteScreen token={token} />;
  },
});

// Pathless layout routes: neither contributes a path segment of its own,
// only a component wrapping <Outlet />, so each needs an explicit `id`
// (there is no path to derive one from).
const authenticatedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "authenticated",
  component: RequireAuth,
});

const shellRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  id: "shell",
  component: AppShell,
});

const indexRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: () => <PlaceholderPage page="Overview" slice={5} />,
});

const moneyGuardRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "money",
  component: () => <RequireCapability cap="money" />,
});
const moneyIndexRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "/",
  component: () => <PlaceholderPage page="Money" slice={2} />,
});
const moneySplatRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "$",
  component: () => <PlaceholderPage page="Money" slice={2} />,
});

const marriageGuardRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "marriage",
  component: () => <RequireCapability cap="marriage" />,
});
const marriageIndexRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "/",
  component: () => <PlaceholderPage page="Marriage" slice={3} />,
});
const marriageSplatRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "$",
  component: () => <PlaceholderPage page="Marriage" slice={3} />,
});

// Family carries no required capability (domain.BuiltinSpaces: "Family is
// unconditional ... it carries no required capability"), so this route sits
// directly under the shell, with no RequireCapability guard.
const familyCalendarRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "family/calendar",
  component: () => <PlaceholderPage page="Family calendar" slice={4} />,
});

const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "settings",
  // Task 20 replaces the placeholder with the real screen -- Members,
  // Spaces, Currency & region and Notifications; Connected accounts is a
  // later slice.
  component: SettingsPage,
});

// Exported (not just `router`) so a test can mount the real tree with its
// own memory history and QueryClient instead of RouterProvider's registered
// singleton -- see routes/router.test.tsx.
export const routeTree = rootRoute.addChildren([
  signInRoute,
  signInMagicRoute,
  inviteRoute,
  signUpRoute,
  signUpCompleteRoute,
  authenticatedRoute.addChildren([
    shellRoute.addChildren([
      indexRoute,
      moneyGuardRoute.addChildren([moneyIndexRoute, moneySplatRoute]),
      marriageGuardRoute.addChildren([marriageIndexRoute, marriageSplatRoute]),
      familyCalendarRoute,
      settingsRoute,
    ]),
  ]),
]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
