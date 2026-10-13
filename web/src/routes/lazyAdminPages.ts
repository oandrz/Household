// The admin pages, loaded lazily.
//
// Lazily loaded so no household member ever downloads the admin bundle --
// requirePlatformAdmin on the server is what actually enforces who may use
// these routes, this only keeps the code itself out of a tab that will
// never be allowed to run it. `.then((m) => ({ default: m.AdminShell }))`
// rather than a default export: both components keep the named-export
// convention every other file in this codebase uses.
//
// They live here, not in router.tsx: React's fast refresh only works on a
// file that exports components and nothing else, and router.tsx also
// exports the route tree and the router. eslint's
// react-refresh/only-export-components rule fails a component declared in
// a file like that.
import { lazy } from "react";

export const LazyAdminShell = lazy(() =>
  import("../features/admin/AdminShell").then((m) => ({
    default: m.AdminShell,
  })),
);
export const LazyAdminFlagsPage = lazy(() =>
  import("../features/admin/AdminFlagsPage").then((m) => ({
    default: m.AdminFlagsPage,
  })),
);
export const LazyAdminMailPage = lazy(() =>
  import("../features/admin/AdminMailPage").then((m) => ({
    default: m.AdminMailPage,
  })),
);
export const LazyAdminMailMessagePage = lazy(() =>
  import("../features/admin/AdminMailPage").then((m) => ({
    default: m.AdminMailMessagePage,
  })),
);
export const LazyAdminHouseholdsPage = lazy(() =>
  import("../features/admin/AdminHouseholdsPage").then((m) => ({
    default: m.AdminHouseholdsPage,
  })),
);
export const LazyAdminHouseholdPage = lazy(() =>
  import("../features/admin/AdminHouseholdPage").then((m) => ({
    default: m.AdminHouseholdPage,
  })),
);
export const LazyAdminDatabasePage = lazy(() =>
  import("../features/admin/AdminDatabasePage").then((m) => ({
    default: m.AdminDatabasePage,
  })),
);
export const LazyAdminDatabaseTablePage = lazy(() =>
  import("../features/admin/AdminDatabasePage").then((m) => ({
    default: m.AdminDatabaseTablePage,
  })),
);
