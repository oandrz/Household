import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { apiFetch, ApiError, setUnauthorizedHandler } from "./client";

function clearCookies() {
  document.cookie.split("; ").forEach((cookie) => {
    const name = cookie.split("=")[0];
    if (name) {
      document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/`;
    }
  });
}

beforeEach(() => clearCookies());
afterEach(() => {
  vi.unstubAllGlobals();
  clearCookies();
  // setUnauthorizedHandler is module-level state, not scoped to a single
  // test -- a handler left installed by one test would otherwise leak into
  // the next and either fire unexpectedly or mask this suite's own
  // assertions about when it should and shouldn't fire.
  setUnauthorizedHandler(null);
});

function stubFetch(status: number, body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify(body), {
          status,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
}

// Captures the RequestInit each call to fetch received, so tests can assert
// on headers, credentials, etc -- the request side of apiFetch, not just the
// response side the tests above already cover.
function stubFetchCapturing(status: number, body: unknown) {
  const fetchMock = vi.fn<
    (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
  >(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("apiFetch", () => {
  it("returns the parsed body on success", async () => {
    stubFetch(200, { status: "ok" });
    await expect(apiFetch<{ status: string }>("/healthz")).resolves.toEqual({
      status: "ok",
    });
  });

  it("throws an ApiError carrying the server's code", async () => {
    stubFetch(401, {
      error: { code: "INVALID_CREDENTIALS", message: "Wrong password." },
    });

    await expect(apiFetch("/api/v1/auth/me")).rejects.toMatchObject({
      code: "INVALID_CREDENTIALS",
      status: 401,
    });
  });

  it("throws ApiError with an UNKNOWN code when the body is not our envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("<html>502</html>", { status: 502 })),
    );

    const error = await apiFetch("/api/v1/anything").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("UNKNOWN");
  });

  it("resolves to undefined on a 204 without throwing", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(null, { status: 204 })),
    );

    await expect(apiFetch("/api/v1/sessions/1")).resolves.toBeUndefined();
  });

  it("throws ApiError with INVALID_RESPONSE on a 200 with an empty body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("", { status: 200 })),
    );

    const error = await apiFetch("/api/v1/anything").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("INVALID_RESPONSE");
    expect((error as ApiError).status).toBe(200);
  });

  it("throws ApiError with INVALID_RESPONSE on a 200 with an HTML body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("<html>not json</html>", { status: 200 })),
    );

    const error = await apiFetch("/api/v1/anything").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("INVALID_RESPONSE");
  });
});

describe("apiFetch request", () => {
  it("sends X-CSRF-Token matching the csrf_token cookie on a POST", async () => {
    document.cookie = "csrf_token=abc123";
    const fetchMock = stubFetchCapturing(200, { status: "ok" });

    await apiFetch("/api/v1/things", { method: "POST", body: "{}" });

    const init = fetchMock.mock.calls[0][1];
    expect((init?.headers as Headers).get("X-CSRF-Token")).toBe("abc123");
  });

  it("does not send X-CSRF-Token on a GET", async () => {
    document.cookie = "csrf_token=abc123";
    const fetchMock = stubFetchCapturing(200, { status: "ok" });

    await apiFetch("/api/v1/things");

    const init = fetchMock.mock.calls[0][1];
    expect((init?.headers as Headers).get("X-CSRF-Token")).toBeNull();
  });

  it("sets Content-Type: application/json when a body is present", async () => {
    const fetchMock = stubFetchCapturing(200, { status: "ok" });

    await apiFetch("/api/v1/things", { method: "POST", body: "{}" });

    const init = fetchMock.mock.calls[0][1];
    expect((init?.headers as Headers).get("Content-Type")).toBe(
      "application/json",
    );
  });

  it("does not set Content-Type when there is no body", async () => {
    const fetchMock = stubFetchCapturing(200, { status: "ok" });

    await apiFetch("/api/v1/things");

    const init = fetchMock.mock.calls[0][1];
    expect((init?.headers as Headers).get("Content-Type")).toBeNull();
  });

  it("always sends credentials: same-origin", async () => {
    const fetchMock = stubFetchCapturing(200, { status: "ok" });

    await apiFetch("/api/v1/things");

    const init = fetchMock.mock.calls[0][1];
    expect(init?.credentials).toBe("same-origin");
  });

  it("transmits a cookie value containing = padding intact", async () => {
    // A realistic base64 CSRF token, padded -- readCookie must not truncate
    // it at the first "=".
    document.cookie = "csrf_token=dGVzdC1jc3JmLXRva2Vu==";
    const fetchMock = stubFetchCapturing(200, { status: "ok" });

    await apiFetch("/api/v1/things", { method: "POST", body: "{}" });

    const init = fetchMock.mock.calls[0][1];
    expect((init?.headers as Headers).get("X-CSRF-Token")).toBe(
      "dGVzdC1jc3JmLXRva2Vu==",
    );
  });
});

// A revoked session (an owner changed a member's capabilities, or removed
// them outright) must be visible to an already-open tab immediately, not
// only once something remounts RequireAuth -- that's the gap this closes.
describe("apiFetch unauthorized handling", () => {
  it("calls the unauthorized handler on a 401 from an ordinary request", async () => {
    stubFetch(401, {
      error: { code: "UNAUTHENTICATED", message: "Sign in required." },
    });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/auth/me").catch(() => {});

    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("does not call the unauthorized handler on a 401 from sign-in itself", async () => {
    // A wrong password is exactly this shape (401 INVALID_CREDENTIALS) --
    // reacting to it would clear the cache and bounce the caller off the
    // sign-in screen they were already using.
    stubFetch(401, {
      error: { code: "INVALID_CREDENTIALS", message: "That email or password is incorrect." },
    });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/auth/sign-in", {
      method: "POST",
      body: "{}",
    }).catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  it("does not call the unauthorized handler on a 401 from magic-link consumption", async () => {
    stubFetch(401, { error: { code: "UNAUTHENTICATED", message: "x" } });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/auth/magic-link/consume", {
      method: "POST",
      body: "{}",
    }).catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  it("does not call the unauthorized handler on a 401 from invite acceptance", async () => {
    stubFetch(401, { error: { code: "UNAUTHENTICATED", message: "x" } });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/invites/some-token/accept", {
      method: "POST",
      body: "{}",
    }).catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  it("does not call the unauthorized handler on a non-401 error", async () => {
    stubFetch(500, { error: { code: "INTERNAL", message: "x" } });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/auth/me").catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  it("stops calling a handler once it has been cleared with null", async () => {
    stubFetch(401, { error: { code: "UNAUTHENTICATED", message: "x" } });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    setUnauthorizedHandler(null);

    await apiFetch("/api/v1/auth/me").catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  // AdminGate exists so an operator whose 30-minute grant lapsed sees a
  // password prompt, not a bounce to /sign-in -- see middleware_admin.go's
  // requireAdminGrant and AdminGate.tsx. That only holds if this handler
  // stays silent for exactly this one code on this one subtree.
  it("does not call the unauthorized handler on ADMIN_REAUTH_REQUIRED from an admin route", async () => {
    stubFetch(401, {
      error: { code: "ADMIN_REAUTH_REQUIRED", message: "Confirm your password." },
    });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/admin/flags").catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  // The same reasoning as ADMIN_REAUTH_REQUIRED above: INVALID_CREDENTIALS is
  // answered by POST /admin/session (a mistyped re-authentication password,
  // admin_reauth.go's Verify), never by a dead session -- AdminGate shows
  // the wrong-password state inline, on the same screen the operator was
  // already using to get back in. ADMIN_LOCKED is not exercised here: it is
  // always a 423, which the `response.status === 401` guard already
  // excludes regardless of code -- a row for it in this describe block would
  // pin that a 423 is a 423, not that the carve-out itself works.
  it("does not call the unauthorized handler on INVALID_CREDENTIALS from POST /admin/session", async () => {
    stubFetch(401, { error: { code: "INVALID_CREDENTIALS", message: "x" } });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/admin/session", { method: "POST", body: "{}" }).catch(() => {});

    expect(handler).not.toHaveBeenCalled();
  });

  // The property the admin-layer carve-out must not weaken: every 401
  // anywhere in the /admin/* chain that means the session itself is gone
  // carries UNAUTHENTICATED, whichever middleware or handler writes it
  // (middleware_session.go, middleware_admin.go, admin_handlers.go) -- that
  // 401 must still bounce the operator to /sign-in exactly like it does
  // everywhere else, admin route or not.
  it("still calls the unauthorized handler on UNAUTHENTICATED from an admin route", async () => {
    stubFetch(401, { error: { code: "UNAUTHENTICATED", message: "Sign in required." } });
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/admin/flags").catch(() => {});

    expect(handler).toHaveBeenCalledTimes(1);
  });

  // Finding 1 (review round 1): ADMIN_LAYER_UNAUTHORIZED_CODES is an
  // allowlist precisely so a 401 this file never wrote the code for --
  // nginx sits in front of this service and can produce one of its own, and
  // a missing or unparseable body reads the same as one -- still fails
  // closed. A denylist keyed on "not UNAUTHENTICATED" would have let this
  // one through: `code === undefined` is not "UNAUTHENTICATED", so it would
  // have been silently exempted and the operator left signed in on a
  // failure nobody's server code produced.
  it("still calls the unauthorized handler on a 401 from an admin route with an unparseable body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("<html>502</html>", { status: 401 })),
    );
    const handler = vi.fn();
    setUnauthorizedHandler(handler);

    await apiFetch("/api/v1/admin/flags").catch(() => {});

    expect(handler).toHaveBeenCalledTimes(1);
  });
});
