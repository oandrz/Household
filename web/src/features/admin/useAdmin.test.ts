// toAdminGateError is the one seam that decides whether a failure this
// module didn't construct (a network error, a body that failed Zod
// validation) opens the real admin surface or closes it -- see its own doc
// comment in useAdmin.ts. It has no test of its own before this file: every
// other admin test only ever hands AdminGate a real ApiError already, so
// this fail-open risk shipped with zero assertions against it.
import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/client";
import { toAdminGateError, isAdminLayerFailure } from "./useAdmin";

describe("toAdminGateError", () => {
  it("passes an ApiError through unchanged", () => {
    const error = new ApiError(404, "NOT_FOUND", "That endpoint does not exist.");
    expect(toAdminGateError(error)).toBe(error);
  });

  it("returns null for a falsy input", () => {
    expect(toAdminGateError(null)).toBeNull();
  });

  // The failure mode this function exists to prevent: a raw `instanceof
  // ApiError` narrowing that maps anything else to `null` would make
  // AdminGate treat an offline browser as "no error", rendering the real
  // admin surface on a request that never actually reached the server.
  it("does not return null for a non-ApiError failure", () => {
    expect(toAdminGateError(new TypeError("offline"))).not.toBeNull();
  });

  it("wraps a non-ApiError failure in a code AdminGate does not recognise", () => {
    const wrapped = toAdminGateError(new TypeError("offline"));
    expect(wrapped).toBeInstanceOf(ApiError);
    expect(["ADMIN_REAUTH_REQUIRED", "INVALID_CREDENTIALS", "ADMIN_LOCKED"]).not.toContain(wrapped?.code);
  });
});

describe("isAdminLayerFailure", () => {
  it("is true for ADMIN_REAUTH_REQUIRED", () => {
    expect(isAdminLayerFailure(new ApiError(401, "ADMIN_REAUTH_REQUIRED", "x"))).toBe(true);
  });

  it("is true for NOT_FOUND", () => {
    expect(isAdminLayerFailure(new ApiError(404, "NOT_FOUND", "x"))).toBe(true);
  });

  it("is false for a business error the caller should render inline", () => {
    expect(isAdminLayerFailure(new ApiError(422, "UNKNOWN_FLAG", "x"))).toBe(false);
  });

  it("is false for a non-ApiError failure", () => {
    expect(isAdminLayerFailure(new TypeError("offline"))).toBe(false);
  });
});
