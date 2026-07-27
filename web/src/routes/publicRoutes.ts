// The one list of things reachable before a session exists.
//
// This used to be two hand-maintained lists with nothing tying either to the
// route tree: preAuthPathPrefixes in api/client.ts and publicRoutePrefixes in
// api/unauthorizedRedirect.ts. HANDOVER.md records that a public route whose
// component calls useMe() reintroduces a bug already fixed once, and sign-up
// added two such routes. publicRoutes.test.ts walks the real tree and fails if
// a route escapes this list.
//
// The two exports are genuinely different things and must not be merged:
// PUBLIC_ROUTE_PREFIXES is about *which screen is mounted*, and
// PRE_AUTH_API_PREFIXES is about *which request path* a 401 must not react to.

// PUBLIC_ROUTE_PREFIXES names every route whose component calls useMe() while
// genuinely reachable with no session -- the sign-in screen checking "is
// someone already signed in", the invite screen checking "are you signed in as
// someone else". "/sign-in" covers /sign-in and /sign-in/magic; "/invite/"
// covers /invite/$token; "/sign-up" covers /sign-up and /sign-up/$token.
export const PUBLIC_ROUTE_PREFIXES = [
  "/sign-in",
  "/invite/",
  "/sign-up",
] as const;

// PRE_AUTH_API_PREFIXES names the request paths a 401 must never react to.
// Every one is reachable before any session exists, so a 401 from one means
// "that specific attempt failed" -- a wrong password, an expired link, a spent
// token -- not "the session this tab thought it had is gone". Reacting there
// would clear the cache and bounce a caller off the very screen they were using
// to establish a session.
//
// /api/v1/auth/sign-up covers the request, preview and complete endpoints with
// one prefix. /api/v1/currencies is here because the sign-up form's currency
// select fetches it before any session exists.
//
// GET /api/v1/auth/me is deliberately absent: that is the exact endpoint whose
// 401 the handler exists to react to. A screen that is reachable pre-auth but
// still calls useMe() is a different problem, solved by PUBLIC_ROUTE_PREFIXES
// above. Do not add "/api/v1/auth/me" here.
export const PRE_AUTH_API_PREFIXES = [
  "/api/v1/auth/sign-in",
  "/api/v1/auth/magic-link",
  "/api/v1/auth/sign-up",
  "/api/v1/invites/",
  "/api/v1/currencies",
] as const;
