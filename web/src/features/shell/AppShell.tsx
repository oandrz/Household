// The authenticated layout: sidebar plus the current page's outlet. Mounted
// only inside RequireAuth's <Outlet />, so `useMe()` here reads the same
// already-resolved ['me'] cache entry RequireAuth just confirmed succeeded --
// this does not perform a second, independent fetch.
import { Outlet } from "@tanstack/react-router";
import { useMe } from "../auth/useAuth";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  const me = useMe();

  if (!me.data) {
    return (
      <main className="grid min-h-screen place-items-center">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  return (
    <div className="grid min-h-screen grid-cols-[236px_1fr] bg-surface">
      <Sidebar me={me.data} />
      <main className="overflow-y-auto">
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
