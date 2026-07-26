import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { apiFetch, ApiError } from "./client";

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
