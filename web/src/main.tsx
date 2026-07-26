import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "./index.css";
import { App } from "./App";
import { setUnauthorizedHandler } from "./api/client";
import { createUnauthorizedHandler } from "./api/unauthorizedRedirect";
import { router } from "./routes/router";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
});

// Wires apiFetch's 401 reaction (see client.ts's isPreAuthRequest / the spec:
// "clear the cache and redirect on 401") to this app's actual QueryClient and
// router. This is the one place that needs both, so it's the one place that
// imports both -- client.ts itself stays free of a react-query or router
// dependency; see setUnauthorizedHandler's own doc comment for why. The
// handler itself lives in unauthorizedRedirect.ts, not inline here, so a
// test can build the identical logic against its own router/QueryClient --
// see that module's doc comment for the regression this factoring exists to
// let a test actually catch.
setUnauthorizedHandler(createUnauthorizedHandler(router, queryClient));

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
