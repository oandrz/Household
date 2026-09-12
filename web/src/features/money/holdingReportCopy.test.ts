import { describe, expect, it } from "vitest";
import { HOLDING_REPORT_COPY, blankReasonCopy, periodHeading, priceAgeLabel } from "./holdingReportCopy";

describe("blankReasonCopy", () => {
  // A blank figure must say WHY. "No figure and a reason" is the PRD's rule,
  // and a dash on its own reads as a defect rather than as missing input.
  it("explains each reason a period cannot be valued", () => {
    expect(blankReasonCopy("no_closing_price")).toMatch(/price/i);
    expect(blankReasonCopy("no_opening_price")).toMatch(/price/i);
    expect(blankReasonCopy("no_closing_price")).not.toEqual(blankReasonCopy("no_opening_price"));
  });

  it("says nothing when there is nothing to explain", () => {
    expect(blankReasonCopy("")).toBe("");
  });
});

describe("periodHeading", () => {
  // A quarter still running is not a result. Labelling it plainly is what
  // stops a half-finished quarter being read as a closed one.
  it("marks the period in progress as to date", () => {
    expect(periodHeading({ label: "Q3 2026", current: true })).toBe("Q3 2026 to date");
    expect(periodHeading({ label: "Q2 2026", current: false })).toBe("Q2 2026");
  });
});

describe("priceAgeLabel", () => {
  const today = "2026-09-12";

  // Valuations going stale is this feature's top product risk, so the age is
  // shown rather than only the date.
  it("counts the days back to the price that measured the period", () => {
    expect(priceAgeLabel("2026-09-12", today)).toBe("priced today");
    expect(priceAgeLabel("2026-09-11", today)).toBe("priced yesterday");
    expect(priceAgeLabel("2026-09-05", today)).toBe("priced 7 days ago");
  });

  // Nothing held at that end means no price was consulted, which is not the
  // same claim as a price that is very old.
  it("says nothing when no price was used", () => {
    expect(priceAgeLabel(null, today)).toBe("");
  });

  // A date the browser cannot parse must not become "priced NaN days ago".
  it("falls back to the raw date rather than arithmetic on nonsense", () => {
    expect(priceAgeLabel("not-a-date", today)).toBe("priced not-a-date");
  });
});

describe("HOLDING_REPORT_COPY", () => {
  it("carries the sentence for a household holding nothing", () => {
    expect(HOLDING_REPORT_COPY.empty).toMatch(/holding/i);
  });
});
