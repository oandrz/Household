import { afterEach, describe, expect, it, vi } from "vitest";
import { timeZoneOptions } from "./timeZoneOptions";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("timeZoneOptions", () => {
  it("offers the zones the browser knows, each labelled with its offset", () => {
    const options = timeZoneOptions([], new Date("2026-10-01T00:00:00Z"));

    const singapore = options.find((o) => o.zone === "Asia/Singapore");
    expect(singapore?.label).toBe("Asia/Singapore (GMT+8)");
    // An underscore is how the zone database spells a space.
    expect(options.find((o) => o.zone === "America/Los_Angeles")?.label).toBe("America/Los Angeles (GMT-7)");
    expect(options.length).toBeGreaterThan(300);
  });

  // The browser's own list leaves UTC out. It is the zone a household gets
  // when nothing else is known, so it has to be selectable.
  it("always offers UTC", () => {
    expect(timeZoneOptions().map((o) => o.zone)).toContain("UTC");
  });

  // A select whose current value is not among its options shows the wrong
  // thing: the browser falls back to the first option, and the household
  // would appear to be somewhere it is not.
  it("always offers the zones it is told must be there, even ones the browser does not list", () => {
    vi.spyOn(Intl, "supportedValuesOf").mockReturnValue(["Asia/Singapore"]);

    const zones = timeZoneOptions(["Asia/Kolkata"]).map((o) => o.zone);

    expect(zones).toEqual(["Asia/Kolkata", "Asia/Singapore", "UTC"]);
  });

  it("lists each zone once and in alphabetical order", () => {
    const zones = timeZoneOptions(["Asia/Singapore", "UTC", ""]).map((o) => o.zone);

    expect(zones).toEqual([...new Set(zones)].sort());
    expect(zones).not.toContain("");
  });

  // A stored zone the browser cannot format still has to show, as itself.
  it("labels a zone the browser cannot format with its bare name", () => {
    const options = timeZoneOptions(["Mars/Olympus_Mons"]);

    expect(options.find((o) => o.zone === "Mars/Olympus_Mons")?.label).toBe("Mars/Olympus Mons");
  });

  it("still offers UTC and the required zones on a browser with no zone list", () => {
    vi.spyOn(Intl, "supportedValuesOf").mockImplementation(() => {
      throw new RangeError("not supported");
    });

    expect(timeZoneOptions(["Asia/Singapore"]).map((o) => o.zone)).toEqual(["Asia/Singapore", "UTC"]);
  });
});
