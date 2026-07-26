import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "./index.css";
import { App } from "./App";
import { setUnauthorizedHandler } from "./api/client";
import { router } from "./routes/router";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
});

// Wires apiFetch's 401 reaction (see client.ts's isPreAuthRequest / the spec:
// "clear the cache and redirect on 401") to this app's actual QueryClient and
// router. This is the one place that needs both, so it's the one place that
// imports both -- client.ts itself stays free of a react-query or router
// dependency; see setUnauthorizedHandler's own doc comment for why.
setUnauthorizedHandler(() => {
  queryClient.clear();
  router.navigate({ to: "/sign-in" });
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
