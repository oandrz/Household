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
//       /            Overview      (the interim Overview: the two of slice 5's
//                                   eight cards Money can supply today, plus a
//                                   setup checklist. It grows into the designed
//                                   page rather than being replaced.)
//       /money       RequireCapability("money") -> Finances (Task 39; slice 2's first real page)
//       /money/transactions RequireCapability("money") -> Transactions (Task 17; the real ledger)
//       /money/budget RequireCapability("money") -> Budget (Task 11; BudgetPage stub -- Task 12 builds the real screen)
//       /money/goals RequireCapability("money") -> Goals (Task 11; the real GoalsPage)
//       /money/bills RequireCapability("money") -> Bills (the real BillsPage)
//       /settings                                   -- the real Settings screen (Task 20)
//
// Marriage and Family have no routes: both were placeholders reading
// "Arriving in slice N", and the sidebar no longer offers them (see
// Sidebar.tsx's SPACE_PAGES). Their URLs fall through to rootRoute's
// notFoundComponent -- and rootRoute sits above the pathless
// authenticatedRoute/shellRoute in this tree, not below them, so that
// fall-through carries two consequences worth knowing before you next touch
// this file. First, the 404 renders shell-less: AppShell never mounts for
// it, so there is no sidebar and (today) no link back anywhere else --
// visiting /marriage is a dead end, not a redirect to a page that offers a
// way out. Second, RequireAuth never runs either, so a signed-out visitor
// with an old /marriage bookmark now reaches bare "Page not found." text
// instead of being bounced to /sign-in the way every real route bounces
// them. Both are accepted for now because these are dead links to a feature
// that doesn't exist yet, not routes anyone should be linking to -- but
// they are exactly the kind of thing that stops being fine the moment
// notFoundComponent grows real content, or a route moves relative to
// authenticatedRoute. Add the route back in the same change that builds the
// page, alongside its SPACE_PAGES entry -- and re-add RequireCapability for
// Marriage, which is capability-gated.
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
import { BillsPage } from "../features/money/BillsPage";
import { BudgetPage } from "../features/money/BudgetPage";
import { FinancesPage } from "../features/money/FinancesPage";
import { GoalsPage } from "../features/money/GoalsPage";
import { TransactionsPage } from "../features/money/TransactionsPage";
import { OverviewPage } from "../features/overview/OverviewPage";
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
  component: OverviewPage,
});

const moneyGuardRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "money",
  component: () => <RequireCapability cap="money" />,
});
const moneyIndexRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "/",
  // Task 39 replaced the Money placeholder with the real Finances screen.
  component: FinancesPage,
});
// Task 17 gives Transactions its own route, a sibling of moneyIndexRoute.
// Nested under moneyGuardRoute, not the shell: a route hung off the shell
// directly would never run RequireCapability at all, and a member without
// `money` would reach a ledger the sidebar never offered them --
// router.test.tsx's own "redirects a member without the money capability
// away from /money/transactions" pins exactly this.
const moneyTransactionsRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "transactions",
  component: TransactionsPage,
});
// A sibling of moneyTransactionsRoute, same reasoning: nested under
// moneyGuardRoute (not the shell) so RequireCapability still runs --
// router.test.tsx's "redirects a member without the money capability away
// from /money/budget" pins the guard, and its positive counterpart pins
// that this route is what actually mounts.
const moneyBudgetRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "budget",
  component: BudgetPage,
});
// A sibling of moneyBudgetRoute, same reasoning: nested under
// moneyGuardRoute (not the shell) so RequireCapability still runs. Task 10
// wired the route to a tiny inline placeholder heading (there was no
// GoalsPage yet); Task 11 replaces it with the real screen, the same swap
// moneyBudgetRoute's own BudgetPage went through. router.test.tsx's
// "redirects a member without the money capability away from /money/goals"
// pins the guard, and "mounts the Goals page at /money/goals" (added
// alongside this change) is moneyBudgetRoute's positive-counterpart pattern,
// restated here -- Task 10's own comment named this test as the one nothing
// would have caught the placeholder swap being forgotten without.
const moneyGoalsRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "goals",
  component: GoalsPage,
});
// A sibling of moneyGoalsRoute, same reasoning: nested under moneyGuardRoute
// (not the shell) so RequireCapability still runs. Bills was
// moneySplatRoute's last remaining reason to exist -- this file's own
// header comment named it -- so this route replaces the splat outright
// rather than sitting beside it: there is no path left under /money/* for a
// catch-all to still be serving. router.test.tsx's "redirects a member
// without the money capability away from /money/bills" pins the guard, and
// "mounts the Bills page at /money/bills" is the positive counterpart
// proving this route (not a vanished splat's placeholder) is what actually
// mounts. BillsPage itself is a shell only here -- Task 12 builds the real
// screen (stat cards, the three lists, five states, archive and restore).
const moneyBillsRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "bills",
  component: BillsPage,
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
      moneyGuardRoute.addChildren([
        moneyIndexRoute,
        moneyTransactionsRoute,
        moneyBudgetRoute,
        moneyGoalsRoute,
        moneyBillsRoute,
      ]),
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
