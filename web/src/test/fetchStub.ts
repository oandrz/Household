// A `vi.stubGlobal("fetch", ...)` stub that matches on "METHOD url" rather
// than call position. A positional stub (return the Nth canned response to
// the Nth call) can't tell the difference between a component calling the
// right endpoint and calling the wrong one in the same order -- every test
// built on it asserts against a canned response, not against behaviour. This
// throws instead of silently returning a mismatched canned value, so a
// component that fetches the wrong thing fails the test loudly rather than
// passing by accident.
import { vi } from "vitest";

export type RouteResponse = { status: number; body: unknown };

// A route may be given one response (returned for every call to it) or a
// list (consumed in order; the last entry repeats once the list runs out --
// convenient for a route a test doesn't care to bound the call count of).
type RouteEntry = RouteResponse | RouteResponse[];

export function stubFetchRoutes(routes: Record<string, RouteEntry>) {
  const queues = new Map<string, RouteResponse[]>(
    Object.entries(routes).map(([key, value]) => [
      key,
      Array.isArray(value) ? [...value] : [value],
    ]),
  );

  const fetchMock = vi.fn<
    (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
  >(async (input, init) => {
    const method = (init?.method ?? "GET").toUpperCase();
    const url = String(input);
    const key = `${method} ${url}`;
    const queue = queues.get(key);
    if (!queue || queue.length === 0) {
      throw new Error(
        `stubFetchRoutes: no stub registered for "${key}" (registered: ${
          [...queues.keys()].join(", ") || "<none>"
        })`,
      );
    }
    const next = queue.length > 1 ? queue.shift()! : queue[0];
    return new Response(JSON.stringify(next.body), {
      status: next.status,
      headers: { "Content-Type": "application/json" },
    });
  });

  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
