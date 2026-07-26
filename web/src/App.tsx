import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./api/client";

// A deliberately minimal shell. The identity plan replaces this with the real
// sidebar; its only job today is to prove the proxy and the stack are wired.
export function App() {
  const health = useQuery({
    queryKey: ["healthz"],
    queryFn: () => apiFetch<{ status: string }>("/healthz"),
  });

  return (
    <main className="min-h-screen grid place-items-center p-10">
      <div className="bg-card border border-hairline rounded-[8px] shadow-[var(--shadow-card)] p-8 max-w-md">
        <h1 className="font-serif text-2xl mb-2">Hearth</h1>
        <p className="text-muted text-sm mb-6">
          Skeleton is running. Identity arrives in the next plan.
        </p>
        <p className="font-mono text-xs">
          API:{" "}
          {health.isPending
            ? "checking…"
            : health.isError
              ? "unreachable"
              : health.data?.status}
        </p>
      </div>
    </main>
  );
}
