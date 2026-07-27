import { describe, expect, it } from "vitest";
import { routeTree } from "./router";
import { PRE_AUTH_API_PREFIXES, PUBLIC_ROUTE_PREFIXES } from "./publicRoutes";

// Walks the real route tree and collects every path that is NOT under the
// pathless `authenticated` layout route. Those are exactly the routes reachable
// with no session, which is the set PUBLIC_ROUTE_PREFIXES has to cover.
//
// HANDOVER.md flagged the two hand-maintained lists as a bug already fixed once:
// a future public route whose component calls useMe() reintroduces it. This test
// is the thing that ties the lists to the tree.
//
// Reads route.fullPath, not route.path: TanStack Router strips the leading
// slash from a route's own path segment (a child route's `path` is "sign-in",
// not "/sign-in") but keeps it on `fullPath` ("/sign-in") -- the same shape
// isOnPublicRoute compares against router.state.location.pathname, which
// always has one. `route.path` is still what filters out the pathless
// `authenticated` layout route below -- it has a `fullPath` of "/" but no
// `path` of its own -- so both fields are read for what each actually tells
// you; using fullPath alone would let that layout route's "/" through as if
// it were itself a public path.
function publicRoutePaths(): string[] {
  const children = (routeTree.children ?? []) as Array<{
    id?: string;
    path?: string;
    fullPath?: string;
  }>;
  return children
    .filter((route) => route.id !== "authenticated" && typeof route.path === "string")
    .map((route) => route.fullPath as string);
}

describe("public routes", () => {
  it("covers every route reachable without a session", () => {
    const uncovered = publicRoutePaths().filter(
      (path) => !PUBLIC_ROUTE_PREFIXES.some((prefix) => path.startsWith(prefix)),
    );
    expect(uncovered).toEqual([]);
  });

  it("lists the sign-up routes", () => {
    const paths = publicRoutePaths();
    expect(paths).toContain("/sign-up");
    expect(paths).toContain("/sign-up/$token");
  });

  it("exempts the sign-up and currency endpoints from the 401 handler", () => {
    expect(PRE_AUTH_API_PREFIXES).toContain("/api/v1/auth/sign-up");
    expect(PRE_AUTH_API_PREFIXES).toContain("/api/v1/currencies");
  });
});
