// The service's own clamps, mirrored so the page can say "50 rows a page"
// without a round trip, and so router.tsx's validateSearch can bound the
// offset a URL asks for. They must match usecase/admin_browse.go's
// BrowseDefaultLimit and BrowseMaxLimit.
//
// Its own module, not a constant in useAdminDatabase.ts, because router.tsx
// imports them and router.tsx may never statically import a hook file: that
// would pull the admin query hooks into the main bundle, which
// adminBundleSplit.test.ts exists to prevent. directoryLimits.ts is the same
// module for the same reason.
export const BROWSE_DEFAULT_LIMIT = 50;
export const BROWSE_MAX_LIMIT = 100;
