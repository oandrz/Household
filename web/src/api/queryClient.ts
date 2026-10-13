// The app's QueryClient settings, in one place so a test can build a client
// that behaves like the real one.
//
// staleTime decides whether a cached answer is shown without asking the
// server again. A test client with the library default (0: always ask
// again) hides every bug that only exists while an answer still counts as
// fresh, which is where a previous person's data would be shown to the next.
import { QueryClient } from "@tanstack/react-query";

export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
  });
}
