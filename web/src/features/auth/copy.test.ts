// limitedAccessClause returns a finished clause rather than a list fragment,
// because the fragment version produced "no access only" on the invite screen
// and "Kid · no only" in Settings for a member holding no capabilities -- a
// state InviteMemberModal reaches with its three toggles all off. These tests
// pin the grammar of every branch, not just the happy one.
import { describe, expect, it } from "vitest";
import { limitedAccessClause } from "./copy";

describe("limitedAccessClause", () => {
  it("names a single capability", () => {
    expect(limitedAccessClause(["calendar"])).toBe("calendar only");
  });

  it("joins several with the design's ampersand, in the order given", () => {
    expect(limitedAccessClause(["calendar", "chores", "money"])).toBe(
      "calendar & chores & money only",
    );
  });

  // The branch the old fragment version got wrong. "no extra access", not
  // "no access": Family is visible to everyone regardless of capability, so
  // this member does share something.
  it("reads as a whole clause when the member holds nothing", () => {
    expect(limitedAccessClause([])).toBe("no extra access");
  });

  // A capability the frontend has no label for must not appear raw in copy,
  // and must not turn the no-capability case into a grammatical one either.
  it("ignores a capability it has no label for", () => {
    expect(limitedAccessClause(["chores", "telepathy"])).toBe("chores only");
    expect(limitedAccessClause(["telepathy"])).toBe("no extra access");
  });
});
