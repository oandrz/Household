// The operator chrome: a header that says "Operator" in as many words, dark
// against every household screen's light canvas, so which surface is on
// screen never depends on reading the URL. This file also owns the one
// AdminGate for the whole /admin subtree -- see its render below for why
// that has to be here, above AppShell-equivalent chrome, rather than inside
// AdminFlagsPage.
//
// requirePlatformAdmin's own doc comment (middleware_admin.go) already
// covers why the 404 exists and what it hides; the property this file has
// to hold up on the frontend is narrower: nothing below the `isPending`
// check may render anything -- not even this header -- until GET
// /api/v1/admin/flags has actually answered. Painting "Operator" first and
// deciding whether the caller belongs here second would let a non-admin's
// browser show, however briefly, a screen that says they might.
import { Link, Outlet, useMatchRoute } from "@tanstack/react-router";
import { AdminGate } from "./AdminGate";
import { toAdminGateError, useAdminFlags, useAdminSession } from "./useAdmin";

function AdminLoadingScreen() {
  return (
    <main className="grid min-h-dvh place-items-center">
      <p className="text-sm text-muted">Loading…</p>
    </main>
  );
}

// Each link states its own single colour class and computes its own active
// state with useMatchRoute -- never `activeProps`, whose className is
// concatenated onto the base and lost to the cascade (docs/LEARNING.md,
// Frontend, the Money links).
function OperatorNav() {
  const matchRoute = useMatchRoute();
  const items = [
    { to: "/admin/flags", label: "Flags" },
    { to: "/admin/households", label: "Households" },
  ] as const;
  return (
    <nav aria-label="Operator" className="flex items-center gap-4">
      {items.map((item) => {
        const active = Boolean(matchRoute({ to: item.to, fuzzy: true }));
        return (
          <Link
            key={item.to}
            to={item.to}
            aria-current={active ? "page" : undefined}
            className={
              active
                ? "text-[12.5px] font-semibold text-white"
                : "text-[12.5px] font-medium text-white/60 hover:text-white"
            }
          >
            {item.label}
          </Link>
        );
      })}
      <Link
        to="/"
        className="text-[12.5px] font-medium text-white/70 hover:text-white"
      >
        Back to Hearth
      </Link>
    </nav>
  );
}

export function AdminShell() {
  const flagsQuery = useAdminFlags();
  const reauth = useAdminSession();

  if (flagsQuery.isPending) return <AdminLoadingScreen />;

  // reauth.error first: it is the freshest signal, and the only one of the
  // two that can carry INVALID_CREDENTIALS or ADMIN_LOCKED (both answered by
  // POST /admin/session, never by GET /admin/flags) -- see useAdmin.ts's own
  // comment on useAdminSession. Falling back to flagsQuery.error covers both
  // the page's first load and a write mutation elsewhere in the subtree that
  // invalidated this same query after discovering the grant had lapsed.
  const error =
    toAdminGateError(reauth.error) ?? toAdminGateError(flagsQuery.error);

  return (
    <AdminGate
      error={error}
      onSubmit={(password) => reauth.mutate(password)}
      pending={reauth.isPending}
    >
      <div className="min-h-dvh bg-canvas">
        <header className="flex items-center justify-between bg-ink px-6 py-3.5 text-white">
          <div className="flex items-center gap-2.5">
            <span
              className="h-2.5 w-2.5 rounded-full bg-accent"
              aria-hidden="true"
            />
            <span className="text-[13.5px] font-semibold tracking-[0.02em]">
              Hearth · Operator
            </span>
          </div>
          <OperatorNav />
        </header>
        <Outlet />
      </div>
    </AdminGate>
  );
}
