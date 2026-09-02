// A leaf on purpose: router.tsx needs these two numbers to build the admin
// households route's default and step-up search params, but router.tsx must
// not statically reach useAdminDirectory.ts to get them -- that hook pulls
// in useAdmin.ts and both admin schema modules, and any of those imported
// from router.tsx would drag the whole admin surface into the main bundle
// (adminBundleSplit.test.ts pins this). This file imports nothing and must
// stay that way.
export const DIRECTORY_DEFAULT_LIMIT = 50;
export const DIRECTORY_MAX_LIMIT = 200;
