// A minimal harness for components that use `<Link>`, `<Navigate>` or
// `useNavigate` -- all three throw outside a router context. Mounts the given
// element as the root route's own component (so it renders regardless of the
// current path) inside a fresh `QueryClient` and a memory-history router.
//
// This router instance is unrelated to the app's real route tree registered
// in `routes/router.tsx` -- `RouterProvider`'s `router` prop is generic over
// whatever router you pass it, so a test-only single-route tree is enough to
// satisfy `Link`/`Navigate` at runtime. Type-checking `to="/money"` etc.
// against real paths comes from the global `Register` module augmentation in
// `routes/router.tsx`, not from this test router, so the two don't need to
// match.
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";

export function renderWithRouter(ui: ReactElement, initialPath = "/") {
  const rootRoute = createRootRoute({ component: () => ui });
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
    router,
    queryClient,
  };
}
