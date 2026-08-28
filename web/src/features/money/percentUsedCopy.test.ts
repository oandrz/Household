import { describe, expect, it } from "vitest";
import { formatPercentUsed } from "./percentUsedCopy";

describe("formatPercentUsed", () => {
  // The defect: S$2.00 against S$800.00 is 0.25%, which domain.PercentUsed
  // correctly rounds to 0 -- and "0% used" reads as "nothing spent" to a
  // household that has spent.
  it("says <1% rather than 0% when something was spent", () => {
    expect(formatPercentUsed(0, 200)).toBe("<1% used");
  });

  // A household that genuinely has not spent must still read 0%, not "<1%",
  // which would be a different lie in the other direction.
  it("says 0% when nothing was spent", () => {
    expect(formatPercentUsed(0, 0)).toBe("0% used");
  });

  it("leaves every other figure alone", () => {
    expect(formatPercentUsed(66, 52800)).toBe("66% used");
    expect(formatPercentUsed(140, 112000)).toBe("140% used");
  });

  // Refunds can exceed the month's spend; PercentUsed is sign-aware and a
  // negative must not be turned into "<1%".
  it("leaves a negative figure alone", () => {
    expect(formatPercentUsed(-3, -2400)).toBe("-3% used");
  });
});
