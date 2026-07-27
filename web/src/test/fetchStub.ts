// A `vi.stubGlobal("fetch", ...)` stub that matches on "METHOD url" rather
// than call position. A positional stub (return the Nth canned response to
// the Nth call) can't tell the difference between a component calling the
// right endpoint and calling the wrong one in the same order -- every test
// built on it asserts against a canned response, not against behaviour. This
// throws instead of silently returning a mismatched canned value, so a
// component that fetches the wrong thing fails the test loudly rather than
// passing by accident.
import { vi } from "vitest";

export type RouteResponse = {
  status: number;
  body: unknown;
  // Optional per-route hook invoked with the parsed request body (JSON.parse'd
  // from `init.body`, or undefined if the request carried none) whenever this
  // route is matched. Kept here, not as a global fetch spy, so the stub stays
  // the single place a test describes the network -- a test that wants to
  // assert what a component posted reads it from the same map it registered
  // the response in, rather than reaching into fetchMock.mock.calls.
  capture?: (body: unknown) => void;
};

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
    if (next.capture) {
      const rawBody = init?.body;
      next.capture(typeof rawBody === "string" ? JSON.parse(rawBody) : rawBody);
    }
    return new Response(JSON.stringify(next.body), {
      status: next.status,
      headers: { "Content-Type": "application/json" },
    });
  });

  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
