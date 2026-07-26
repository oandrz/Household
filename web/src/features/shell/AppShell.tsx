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
        <Outlet />
      </main>
    </div>
  );
}
