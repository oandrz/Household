// monthLabel only formats a month it is given. Which month it is now is the
// household's (lib/householdDate.ts), and is tested there.
import { describe, expect, it } from "vitest";
import { monthLabel } from "./month";

describe("monthLabel", () => {
  it("names the month and year a YYYY-MM string stands for", () => {
    expect(monthLabel("2026-07")).toBe("July 2026");
  });

  it("does not slip into the previous month at the start of the year", () => {
    expect(monthLabel("2026-01")).toBe("January 2026");
  });
});
