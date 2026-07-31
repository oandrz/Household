// Pinned against toISOString(), not merely against "returns a string": a UTC
// conversion reads back the wrong month for any household east or west of UTC
// near a month boundary, which is the mistake this project has now made three
// times (see BudgetPage.tsx:52 and AccountModal.tsx's today()).
import { afterEach, describe, expect, it, vi } from "vitest";
import { currentMonth } from "./month";

afterEach(() => {
  vi.useRealTimers();
});

describe("currentMonth", () => {
  it("reads the local calendar month, not the UTC one", () => {
    // 1 August 2026, 07:00 in UTC+8 -- still 31 July in UTC. A household in
    // Singapore is in August; toISOString() would say July.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-31T23:00:00.000Z"));
    const localMonth = `${new Date().getFullYear()}-${String(new Date().getMonth() + 1).padStart(2, "0")}`;

    expect(currentMonth()).toBe(localMonth);
  });

  it("pads a single-digit month", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 15));   // March, local
    expect(currentMonth()).toBe("2026-03");
  });
});
