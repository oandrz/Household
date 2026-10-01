// Zod schema tests live here because nothing else in the repo calls
// meQuerySchema.parse() directly -- every component/hook test satisfies the
// `Me` type via a hand-built fixture object, which already includes every
// field the type demands. That is fine for testing components, but it means
// none of those 682 other tests can prove anything about what the schema
// does with a wire body that is missing a field -- they never run untyped
// JSON through .parse() at all.
import { describe, expect, it } from "vitest";
import { meQuerySchema } from "./schemas";

// Shaped like a body from a server build that predates isPlatformAdmin and
// features -- omits both keys entirely, the way a real pre-migration server
// (or a stale cached response) would, rather than sending them as
// `undefined` or `null`.
function meBodyWithoutNewFields(): unknown {
  return {
    user: { id: "u1", email: "a@b.c", displayName: "A", avatarInitial: "A" },
    household: {
      id: "h1",
      name: "H",
      familyName: "H",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "manual",
    },
    membership: {
      id: "m1",
      householdId: "h1",
      userId: "u1",
      role: "owner",
      capabilities: [],
    },
    capabilities: [],
    spaces: [],
    // isPlatformAdmin and features deliberately absent.
  };
}

describe("meQuerySchema", () => {
  // This is the entire reason isPlatformAdmin/features are `.default(...)`
  // rather than required: an older server that hasn't been redeployed yet
  // must not make the whole bundle fail to parse, because a parse failure
  // inside fetchMe (useAuth.ts) throws and signs the caller out. Both
  // defaults are the closed state, matching what an admin-unaware server
  // implicitly means for both.
  it("parses a body from a server that predates isPlatformAdmin and features, defaulting both to the closed state", () => {
    const result = meQuerySchema.parse(meBodyWithoutNewFields());

    expect(result.isPlatformAdmin).toBe(false);
    expect(result.features).toEqual({});
  });

  // The fixture above also omits household.timezone. A server that old works
  // out every date in UTC, so UTC is the zone that keeps the frontend's
  // "today" equal to that server's.
  it("reads a household with no time zone as UTC, and keeps one that is sent", () => {
    expect(meQuerySchema.parse(meBodyWithoutNewFields()).household.timezone).toBe("UTC");

    const body = meBodyWithoutNewFields() as { household: Record<string, unknown> };
    body.household.timezone = "Asia/Singapore";
    expect(meQuerySchema.parse(body).household.timezone).toBe("Asia/Singapore");
  });
});
