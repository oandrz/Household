// The authenticated layout: navigation plus the current page's outlet.
// Mounted only inside RequireAuth's <Outlet />, so `useMe()` here reads the
// same already-resolved ['me'] cache entry RequireAuth just confirmed
// succeeded -- this does not perform a second, independent fetch.
//
// Below `lg` the navigation is off-canvas (NavDrawer) and reached through
// MobileTopBar; at `lg` and above the grid below is exactly the two-column
// layout this file has always had, because NavDrawer resolves to
// `display: contents` at that width and stops generating a box.
import { useEffect, useState } from "react";
import { Outlet, useRouterState } from "@tanstack/react-router";
import { useMe } from "../auth/useAuth";
import { MobileTopBar } from "./MobileTopBar";
import { NavDrawer } from "./NavDrawer";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  const me = useMe();
  const [navOpen, setNavOpen] = useState(false);
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  // A tapped link must land the household on the new page, not behind an
  // overlay still covering it. Keyed on the pathname rather than on the link
  // press, so a route change from anywhere -- a redirect, a programmatic
  // navigate -- closes it too.
  useEffect(() => {
    setNavOpen(false);
  }, [pathname]);

  if (!me.data) {
    return (
      <main className="grid min-h-dvh place-items-center">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  return (
    <div className="min-h-dvh bg-surface lg:grid lg:grid-cols-[236px_1fr]">
      <MobileTopBar onOpenNav={() => setNavOpen(true)} />
      <NavDrawer open={navOpen} onClose={() => setNavOpen(false)}>
        <Sidebar me={me.data} />
      </NavDrawer>
      {/* `inert` keeps Tab out of the page while the drawer's opaque backdrop
          covers it -- the one part of a <dialog>'s behaviour that cannot be
          expressed in CSS. React 19 forwards it as a real attribute. */}
      <main inert={navOpen || undefined} className="lg:overflow-y-auto">
        {/* 1204px is the design's own content width, not a taste: its canvas
            is 1440px wide with a 236px sidebar (design/Household
            Dashboard.dc.html, screen 5a). Without this, `1fr` gives every
            page the whole monitor -- on a 2752px display the ledger's heading
            and its "+ Add transaction" button ended up 2400px apart, and a
            Settings toggle sat 1200px from the label naming it. Pages keep
            their own padding; this element only bounds them. */}
        <div className="mx-auto w-full max-w-[1204px]">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
