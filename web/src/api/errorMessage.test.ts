import { describe, expect, it } from "vitest";
import { ApiError } from "./client";
import { apiErrorMessage } from "./errorMessage";

describe("apiErrorMessage", () => {
  it("shows the server's own message for an ApiError", () => {
    const err = new ApiError(409, "BILL_NAME_TAKEN", "You already have a bill called that.");
    expect(apiErrorMessage(err, "Something went wrong.")).toBe("You already have a bill called that.");
  });

  it("falls back for anything that is not an ApiError, so a screen never shows nothing", () => {
    expect(apiErrorMessage(new TypeError("Failed to fetch"), "Something went wrong.")).toBe(
      "Something went wrong.",
    );
    expect(apiErrorMessage(undefined, "Something went wrong.")).toBe("Something went wrong.");
  });
});
