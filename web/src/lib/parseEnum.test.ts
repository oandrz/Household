import { describe, expect, it } from "vitest";
import { parseEnum } from "./parseEnum";

const CADENCES = ["one_off", "monthly", "quarterly", "yearly"] as const;

describe("parseEnum", () => {
  it("passes through a value that is in the allowed set", () => {
    expect(parseEnum("quarterly", CADENCES, "monthly")).toBe("quarterly");
  });

  it("keeps the fallback for a value outside the set, instead of letting it through typed as valid", () => {
    expect(parseEnum("weekly", CADENCES, "monthly")).toBe("monthly");
    expect(parseEnum("", CADENCES, "monthly")).toBe("monthly");
  });
});
