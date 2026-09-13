// The full-screen "Loading…" placeholder, shared by AdminShell (while its gate
// query is in flight) and router.tsx (while a lazy admin chunk downloads). A
// leaf with no imports on purpose: router.tsx is in main.tsx's static import
// graph, and adminBundleSplit.test.ts fails if anything it pulls in reaches an
// admin file.
export function LoadingScreen() {
  return (
    <main className="grid min-h-dvh place-items-center">
      <p className="text-sm text-muted">Loading…</p>
    </main>
  );
}
