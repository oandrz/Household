// The point of every test here is that the browser's own time zone must not
// matter. So the "browser" is in Los Angeles, the household is somewhere
// else, and the clock is stopped at an instant where the two are on
// different dates.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { longDateIn, monthIn, monthNameIn, todayIn, yearIn } from "./householdDate";

const ORIGINAL_TZ = process.env.TZ;

beforeEach(() => {
  process.env.TZ = "America/Los_Angeles";
  vi.useFakeTimers({ toFake: ["Date"] });
});

afterEach(() => {
  vi.useRealTimers();
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
});

describe("todayIn", () => {
  it("is the household's date, not the browser's and not UTC's", () => {
    // 23:00 UTC on 30 September: 16:00 on the 30th in Los Angeles, and
    // already 07:00 on 1 October in Singapore.
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));

    expect(todayIn("Asia/Singapore")).toBe("2026-10-01");
    expect(todayIn("UTC")).toBe("2026-09-30");
    expect(todayIn("America/Los_Angeles")).toBe("2026-09-30");
  });

  it("is a day behind UTC for a household west of it", () => {
    vi.setSystemTime(new Date("2026-10-01T03:00:00Z"));

    expect(todayIn("America/Los_Angeles")).toBe("2026-09-30");
    expect(todayIn("UTC")).toBe("2026-10-01");
  });

  it("covers both edges of the map", () => {
    // Kiritimati is UTC+14 and Pago Pago is UTC-11, so at 10:30 UTC they
    // are two calendar days apart, on either side of UTC's date.
    vi.setSystemTime(new Date("2026-12-31T10:30:00Z"));

    expect(todayIn("Pacific/Kiritimati")).toBe("2027-01-01");
    expect(todayIn("UTC")).toBe("2026-12-31");
    expect(todayIn("Pacific/Pago_Pago")).toBe("2026-12-30");
  });

  it("pads a single-digit month and day", () => {
    vi.setSystemTime(new Date("2026-03-05T12:00:00Z"));

    expect(todayIn("Asia/Singapore")).toBe("2026-03-05");
  });

  // Falling back to the browser's zone here would quietly bring back the
  // defect this file exists to remove, so an unusable zone is an error.
  it("throws on a zone it cannot use, and never falls back to the browser's", () => {
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));

    expect(() => todayIn("Mars/Olympus_Mons")).toThrow();
    expect(() => todayIn("")).toThrow();
  });
});

describe("monthIn", () => {
  it("is the household's month on the first morning of a month", () => {
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));

    expect(monthIn("Asia/Singapore")).toBe("2026-10");
    expect(monthIn("UTC")).toBe("2026-09");
  });
});

describe("monthNameIn", () => {
  it("names the household's month", () => {
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));

    expect(monthNameIn("Asia/Singapore")).toBe("October");
    expect(monthNameIn("America/Los_Angeles")).toBe("September");
  });
});

describe("yearIn", () => {
  it("is the household's year on New Year's morning", () => {
    vi.setSystemTime(new Date("2026-12-31T20:00:00Z"));

    expect(yearIn("Asia/Singapore")).toBe(2027);
    expect(yearIn("UTC")).toBe(2026);
  });
});

describe("longDateIn", () => {
  it("writes out the household's date for a person to read", () => {
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));

    expect(longDateIn("Asia/Singapore")).toBe("Thu, 1 Oct 2026");
    expect(longDateIn("Pacific/Pago_Pago")).toBe("Wed, 30 Sep 2026");
  });
});
