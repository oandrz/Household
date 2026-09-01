// The one "this doesn't exist" page, shared by two callers that must render
// byte-identical output: rootRoute's own notFoundComponent (router.tsx), for
// a URL that matches no route at all, and AdminGate (features/admin/), for a
// signed-in non-admin visiting /admin -- requirePlatformAdmin's own 404 is
// built to be indistinguishable from a route that was never registered, and
// two copies of the same three lines of markup could drift out of sync with
// no test able to see it. A single component makes that structural rather
// than a fact someone has to remember.
//
// Router-free on purpose: AdminGate's own tests mount this with no
// RouterProvider in the tree, so this must never reach for a `<Link>` or any
// other router hook.
export function NotFoundScreen() {
  return (
    <main className="grid min-h-dvh place-items-center p-10 text-center">
      <p className="text-sm text-muted">Page not found.</p>
    </main>
  );
}
