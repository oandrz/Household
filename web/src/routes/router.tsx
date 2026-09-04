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
//       /marriage/retros RequireCapability("marriage") -> Retros (Task 10; RetrosPage)
//       /marriage/vision RequireCapability("marriage") -> Vision & goals (Task 11; VisionPage)
//       /settings                                   -- the real Settings screen (Task 20)
//     /admin       AdminShell (own chrome, no AppShell)  -> /admin/flags
//       /admin/flags                                -- AdminFlagsPage
//       /admin/mail                                 -- AdminMailPage (Task 7; the outbound mail list)
//       /admin/mail/$messageId                      -- AdminMailMessagePage (Task 7; one message)
//       /admin/households                           -- AdminHouseholdsPage (Task 8; tiles + search + rows)
//       /admin/households/$householdId              -- AdminHouseholdPage (Task 9; the drill-in)
//       /admin/database                             -- AdminDatabasePage (the table list)
//       /admin/database/$table                      -- AdminDatabaseTablePage (one page of one table; ?limit=&offset=)
//
// The /admin subtree sits directly under authenticatedRoute, a sibling of
// shellRoute rather than a child of it: the admin surface has its own
// chrome (AdminShell says "Operator", AppShell's sidebar does not apply),
// and RequireAuth is still the guard that matters here -- a caller with no
// session at all gets bounced to /sign-in exactly like every other route,
// while requirePlatformAdmin on the server is what actually decides whether
// an authenticated caller belongs on /admin at all (a 404, indistinguishable
// from a route that was never registered -- see AdminGate.tsx). Both
// components are behind React.lazy, so the code never reaches a household
// member's bundle even though the route itself is always registered
// (adminBundleSplit.test.ts pins the second half of that: registering the
// route must not accidentally turn the lazy import back into a static one).
//
// Marriage came back in Task 10, in the same change as its SPACE_PAGES entry
// (Sidebar.tsx) and its RequireCapability guard -- 110ab0a deleted all three
// together for the reason its own commit message gives (a nav row whose only
// content is "Arriving in slice N" reads as broken), so nothing about that
// reasoning is undone by adding the route back alone; the guard and the
// sidebar entry have to land with it. marriageGuardRoute had one child at
// that point (retros) and no index child, unlike moneyGuardRoute's own
// moneyIndexRoute (path "/") which gives bare "/money" a real page -- a
// caller who typed bare "/marriage" by hand matched marriageGuardRoute
// itself (a real route, not an absence), ran RequireAuth and
// RequireCapability as normal, and then had no child route left to render:
// AppShell's sidebar showed, but the content area was blank rather than a
// page or a 404. That was accepted as a known minor gap at the time, not a
// redirect worth adding speculatively, on the reasoning that an index route
// belongs to whichever task first gives Marriage a real landing page
// distinct from Retros -- Task 11 is that task: Vision & goals is Marriage's
// second page, so marriageIndexRoute now redirects bare "/marriage" to
// "/marriage/retros", the same "first page wins" choice moneyIndexRoute
// already made for Money. Family still has no route at all: its own
// "Arriving in slice 4" placeholder was deleted in the same commit as
// Marriage's and nothing has rebuilt it yet, so /family/calendar still
// genuinely falls through to rootRoute's notFoundComponent the way this file
// used to say every Marriage URL did too.
import { Suspense, lazy } from "react";
import {
  Navigate,
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
// Both limit pairs come from their own leaf modules, never from the hook
// files that also re-export them: a static import of an admin hook here
// would drag the whole admin query layer into main.tsx's bundle, which
// adminBundleSplit.test.ts exists to prevent.
import {
  BROWSE_DEFAULT_LIMIT,
  BROWSE_MAX_LIMIT,
} from "../features/admin/browseLimits";
import {
  DIRECTORY_DEFAULT_LIMIT,
  DIRECTORY_MAX_LIMIT,
} from "../features/admin/directoryLimits";
import { InviteScreen } from "../features/auth/InviteScreen";
import { MagicLinkConsumeScreen } from "../features/auth/MagicLinkConsumeScreen";
import { SignInScreen } from "../features/auth/SignInScreen";
import { SignUpScreen } from "../features/auth/SignUpScreen";
import { SignUpCompleteScreen } from "../features/auth/SignUpCompleteScreen";
import { useMe } from "../features/auth/useAuth";
import { RetrosPage } from "../features/marriage/RetrosPage";
import { VisionPage } from "../features/marriage/VisionPage";
import { BillsPage } from "../features/money/BillsPage";
import { BudgetPage } from "../features/money/BudgetPage";
import { FinancesPage } from "../features/money/FinancesPage";
import { GoalsPage } from "../features/money/GoalsPage";
import { TransactionsPage } from "../features/money/TransactionsPage";
import { OverviewPage } from "../features/overview/OverviewPage";
import { SettingsPage } from "../features/settings/SettingsPage";
import { AppShell } from "../features/shell/AppShell";
import { NotFoundScreen } from "../features/shell/NotFoundScreen";
import { RequireAuth } from "../features/shell/RequireAuth";
import { RequireCapability } from "../features/shell/RequireCapability";

const rootRoute = createRootRoute({
  notFoundComponent: NotFoundScreen,
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

// Marriage's own guard, the moneyGuardRoute shape restated: a pathless route
// whose only job is to run RequireCapability before any child mounts.
// Nested under shellRoute, same as moneyGuardRoute -- so its own guard
// actually runs, the identical reasoning moneyTransactionsRoute's comment
// gives for why a route needs to sit under its capability's guard rather
// than beside it.
const marriageGuardRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "marriage",
  component: () => <RequireCapability cap="marriage" />,
});
// Retros is Marriage's first page (Task 10). Vision & goals is its second
// (Task 11) -- Agreements (docs/FEATURE_TRACKER.md section 6) will get its
// own sibling route under marriageGuardRoute when it's built, the same way
// moneyBudgetRoute and moneyGoalsRoute joined moneyIndexRoute one at a time.
const marriageRetrosRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "retros",
  component: RetrosPage,
});
const marriageVisionRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "vision",
  component: VisionPage,
});

// Task 10 left marriageGuardRoute with one child and no index, so bare
// "/marriage" rendered the shell with an empty content area (this file's
// own header comment has the full history). Marriage now has two pages, so
// the index goes to the first of them -- moneyIndexRoute's own "first page
// wins" choice, restated for Marriage now that it has a "first page" to
// redirect to at all.
const marriageIndexRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/marriage/retros" });
  },
});

const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "settings",
  // Task 20 replaces the placeholder with the real screen -- Members,
  // Spaces, Currency & region and Notifications; Connected accounts is a
  // later slice.
  component: SettingsPage,
});

// Lazily loaded so no household member ever downloads the admin bundle --
// requirePlatformAdmin on the server is what actually enforces who may use
// these routes, this only keeps the code itself out of a tab that will
// never be allowed to run it. `.then((m) => ({ default: m.AdminShell }))`
// rather than a default export: both components keep the named-export
// convention every other file in this codebase uses.
const LazyAdminShell = lazy(() =>
  import("../features/admin/AdminShell").then((m) => ({
    default: m.AdminShell,
  })),
);
const LazyAdminFlagsPage = lazy(() =>
  import("../features/admin/AdminFlagsPage").then((m) => ({
    default: m.AdminFlagsPage,
  })),
);
const LazyAdminMailPage = lazy(() =>
  import("../features/admin/AdminMailPage").then((m) => ({
    default: m.AdminMailPage,
  })),
);
const LazyAdminMailMessagePage = lazy(() =>
  import("../features/admin/AdminMailPage").then((m) => ({
    default: m.AdminMailMessagePage,
  })),
);
const LazyAdminHouseholdsPage = lazy(() =>
  import("../features/admin/AdminHouseholdsPage").then((m) => ({
    default: m.AdminHouseholdsPage,
  })),
);
const LazyAdminHouseholdPage = lazy(() =>
  import("../features/admin/AdminHouseholdPage").then((m) => ({
    default: m.AdminHouseholdPage,
  })),
);
const LazyAdminDatabasePage = lazy(() =>
  import("../features/admin/AdminDatabasePage").then((m) => ({
    default: m.AdminDatabasePage,
  })),
);
const LazyAdminDatabaseTablePage = lazy(() =>
  import("../features/admin/AdminDatabasePage").then((m) => ({
    default: m.AdminDatabaseTablePage,
  })),
);

// The Suspense fallback below is inlined at both call sites rather than
// factored into its own top-level component, matching signInMagicRoute's
// own comment on why: a named module-scope component here would trip
// eslint-plugin-react-refresh's only-export-components rule against this
// file's other exports (routeTree, router), even unexported. It matches
// AdminShell's own AdminLoadingScreen -- this one covers the network
// round-trip for the chunk itself, before AdminShell's module (and the
// query it starts) has even arrived, so a household member watching the
// network tab sees the identical "a page is loading" moment either place a
// route suspends.
const adminRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "admin",
  component: () => (
    <Suspense
      fallback={
        <main className="grid min-h-dvh place-items-center">
          <p className="text-sm text-muted">Loading…</p>
        </main>
      }
    >
      <LazyAdminShell />
    </Suspense>
  ),
});

// Bare "/admin" gets a real page the same way moneyIndexRoute and
// marriageIndexRoute give their own space's bare URL one -- Flags is the
// only admin page today, so it is the "first page wins" redirect target by
// default, not a considered ranking.
const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/admin/flags" });
  },
});

const adminFlagsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "flags",
  component: () => (
    <Suspense
      fallback={
        <main className="grid min-h-dvh place-items-center">
          <p className="text-sm text-muted">Loading…</p>
        </main>
      }
    >
      <LazyAdminFlagsPage />
    </Suspense>
  ),
});

// The list takes no URL state (decision 8: no search, one fixed page size),
// so unlike adminHouseholdsRoute below it needs neither validateSearch nor a
// route component of its own beyond the Suspense boundary every lazy admin
// route repeats.
const adminMailRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "mail",
  component: () => (
    <Suspense
      fallback={
        <main className="grid min-h-dvh place-items-center">
          <p className="text-sm text-muted">Loading…</p>
        </main>
      }
    >
      <LazyAdminMailPage />
    </Suspense>
  ),
});

const adminMailMessageRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "mail/$messageId",
  // Named, not an inline arrow, for the identical rules-of-hooks reason
  // adminHouseholdRoute's own component gives below.
  component: function AdminMailMessageRouteComponent() {
    const { messageId } = useParams({
      from: "/authenticated/admin/mail/$messageId",
    });
    return (
      <Suspense
        fallback={
          <main className="grid min-h-dvh place-items-center">
            <p className="text-sm text-muted">Loading…</p>
          </main>
        }
      >
        <LazyAdminMailMessagePage messageId={messageId} />
      </Suspense>
    );
  },
});

// The households list keeps its search and limit in the URL, so reload,
// back and the audit row all agree on what was shown. The page itself
// takes them as props and hands navigation back here -- the same split
// signInMagicRoute makes for its token.
const adminHouseholdsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "households",
  validateSearch: (
    search: Record<string, unknown>,
  ): { q: string; limit: number } => ({
    q: typeof search.q === "string" ? search.q : "",
    limit:
      typeof search.limit === "number" &&
      Number.isInteger(search.limit) &&
      search.limit > 0
        ? search.limit
        : DIRECTORY_DEFAULT_LIMIT,
  }),
  // Named, not an inline arrow, for the identical reason
  // signInMagicRoute's own comment gives: this one calls hooks directly
  // (useSearch, useNavigate), and eslint-plugin-react-hooks only recognises
  // a function as a component -- and so allows it to call hooks -- by its
  // name starting with an uppercase letter.
  component: function AdminHouseholdsRouteComponent() {
    // "/authenticated/admin/households", not the public "/admin/households"
    // URL: useSearch's `from` takes a route ID, and authenticatedRoute is a
    // pathless route (id "authenticated", no `path`) -- its id still joins
    // the chain that identifies a route, even though it contributes nothing
    // to the URL a caller actually types or a <Link to> resolves against.
    const { q, limit } = useSearch({ from: "/authenticated/admin/households" });
    const navigate = useNavigate();
    return (
      <Suspense
        fallback={
          <main className="grid min-h-dvh place-items-center">
            <p className="text-sm text-muted">Loading…</p>
          </main>
        }
      >
        <LazyAdminHouseholdsPage
          q={q}
          limit={limit}
          onSearch={(next) =>
            navigate({
              to: "/admin/households",
              search: { q: next, limit: DIRECTORY_DEFAULT_LIMIT },
            })
          }
          onShowMore={() =>
            navigate({
              to: "/admin/households",
              search: {
                q,
                limit: Math.min(limit * 2, DIRECTORY_MAX_LIMIT),
              },
            })
          }
        />
      </Suspense>
    );
  },
});

const adminHouseholdRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "households/$householdId",
  // Named for the same rules-of-hooks reason as adminHouseholdsRoute's own
  // component just above.
  component: function AdminHouseholdRouteComponent() {
    // Same route-ID-vs-URL-path distinction as adminHouseholdsRoute's own
    // useSearch above.
    const { householdId } = useParams({
      from: "/authenticated/admin/households/$householdId",
    });
    return (
      <Suspense
        fallback={
          <main className="grid min-h-dvh place-items-center">
            <p className="text-sm text-muted">Loading…</p>
          </main>
        }
      >
        <LazyAdminHouseholdPage householdId={householdId} />
      </Suspense>
    );
  },
});

// The table list takes no URL state (one fixed page size, no search), so
// like adminMailRoute it needs neither validateSearch nor a route component
// of its own beyond the Suspense boundary every lazy admin route repeats.
const adminDatabaseRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "database",
  component: () => (
    <Suspense
      fallback={
        <main className="grid min-h-dvh place-items-center">
          <p className="text-sm text-muted">Loading…</p>
        </main>
      }
    >
      <LazyAdminDatabasePage />
    </Suspense>
  ),
});

// The row viewer keeps its page in the URL, so reload, Back and the audit
// row all agree on what was shown -- the households list's own reasoning,
// and it matters more here: this route's audit row is the record that
// somebody read a particular page of a particular table.
//
// validateSearch is the only thing standing between a hand-typed URL and a
// request the service would have to refuse: a non-integer, a zero or a
// negative limit becomes the default, an oversized one is clamped to
// BROWSE_MAX_LIMIT (the service's own cap, mirrored in browseLimits.ts), and
// a negative offset becomes 0. TanStack parses ?limit=50 as the number 50,
// so `typeof === "number"` is the real check, not a string coercion.
const adminDatabaseTableRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "database/$table",
  validateSearch: (
    search: Record<string, unknown>,
  ): { limit: number; offset: number } => ({
    limit:
      typeof search.limit === "number" &&
      Number.isInteger(search.limit) &&
      search.limit > 0
        ? Math.min(search.limit, BROWSE_MAX_LIMIT)
        : BROWSE_DEFAULT_LIMIT,
    offset:
      typeof search.offset === "number" &&
      Number.isInteger(search.offset) &&
      search.offset >= 0
        ? search.offset
        : 0,
  }),
  // Named, not an inline arrow, for the identical rules-of-hooks reason
  // adminHouseholdsRoute's own component gives above.
  component: function AdminDatabaseTableRouteComponent() {
    // Route IDs, not the public URL path -- same distinction
    // adminHouseholdsRoute's own useSearch comment draws.
    const { table } = useParams({
      from: "/authenticated/admin/database/$table",
    });
    const { limit, offset } = useSearch({
      from: "/authenticated/admin/database/$table",
    });
    const navigate = useNavigate();
    return (
      <Suspense
        fallback={
          <main className="grid min-h-dvh place-items-center">
            <p className="text-sm text-muted">Loading…</p>
          </main>
        }
      >
        <LazyAdminDatabaseTablePage
          table={table}
          limit={limit}
          offset={offset}
          onPage={(nextOffset) =>
            navigate({
              to: "/admin/database/$table",
              params: { table },
              // limit travels with the offset: dropping it would silently
              // reset a clamped or widened page size on every page turn.
              search: { limit, offset: nextOffset },
            })
          }
        />
      </Suspense>
    );
  },
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
      marriageGuardRoute.addChildren([
        marriageIndexRoute,
        marriageRetrosRoute,
        marriageVisionRoute,
      ]),
      settingsRoute,
    ]),
    // A sibling of shellRoute, not nested inside it: the admin surface does
    // not get AppShell's sidebar (this file's own header comment explains
    // why).
    adminRoute.addChildren([
      adminIndexRoute,
      adminFlagsRoute,
      adminMailRoute,
      adminMailMessageRoute,
      adminHouseholdsRoute,
      adminHouseholdRoute,
      adminDatabaseRoute,
      adminDatabaseTableRoute,
    ]),
  ]),
]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
