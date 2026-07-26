import { describe, expect, it, vi, afterEach } from "vitest";
import { apiFetch, ApiError } from "./client";

afterEach(() => vi.unstubAllGlobals());

function stubFetch(status: number, body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
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

    const error = await apiFetch("/api/v1/anything").catch((e) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error.code).toBe("UNKNOWN");
  });
});
